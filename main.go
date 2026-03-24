package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/miekg/dns"
)

const (
	// Default DNS port
	defaultPort = "53"
	// Timeout for DNS queries
	queryTimeout = 5 * time.Second
	// Maximum recursion depth
	maxRecursionDepth = 15
	// Minimum cache TTL (for records with very short TTL)
	minCacheTTL = 10 * time.Second
	// Maximum cache TTL (cap very long TTLs)
	maxCacheTTL = 1 * time.Hour
	// Temporary penalty for nameservers that recently timed out or errored
	badNameserverTTL = 1 * time.Minute
)

// Root DNS servers (a.root-servers.net through m.root-servers.net)
var rootServers = []string{
	"198.41.0.4:53",     // a.root-servers.net
	"199.9.14.201:53",   // b.root-servers.net
	"192.33.4.12:53",    // c.root-servers.net
	"199.7.91.13:53",    // d.root-servers.net
	"192.203.230.10:53", // e.root-servers.net
	"192.5.5.241:53",    // f.root-servers.net
	"192.112.36.4:53",   // g.root-servers.net
	"198.97.190.53:53",  // h.root-servers.net
	"192.36.148.17:53",  // i.root-servers.net
	"192.58.128.30:53",  // j.root-servers.net
	"193.0.14.129:53",   // k.root-servers.net
	"199.7.83.42:53",    // l.root-servers.net
	"202.12.27.33:53",   // m.root-servers.net
}

type cacheEntry struct {
	response   *dns.Msg
	timestamp  time.Time
	expiration time.Time
}

type nsCacheEntry struct {
	addresses  []string
	expiration time.Time
}

type queryContext struct {
	inFlight map[string]bool
	mu       sync.Mutex
}

func (qc *queryContext) isInFlight(key string) bool {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	return qc.inFlight[key]
}

func (qc *queryContext) markInFlight(key string) {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	qc.inFlight[key] = true
}

func (qc *queryContext) unmarkInFlight(key string) {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	delete(qc.inFlight, key)
}

type DNSServer struct {
	client         *dns.Client
	cache          map[string]*cacheEntry
	nsCache        map[string]*nsCacheEntry
	badNameservers map[string]time.Time
	mu             sync.RWMutex
	ipv6Available  bool
}

// Common TLD nameserver IPs to avoid circular dependencies
var tldHints = map[string][]string{
	"a.gtld-servers.net.": {"192.5.6.30"},
	"b.gtld-servers.net.": {"192.33.14.30"},
	"c.gtld-servers.net.": {"192.26.92.30"},
	"d.gtld-servers.net.": {"192.31.80.30"},
	"e.gtld-servers.net.": {"192.12.94.30"},
	"f.gtld-servers.net.": {"192.35.51.30"},
	"g.gtld-servers.net.": {"192.42.93.30"},
	"h.gtld-servers.net.": {"192.54.112.30"},
	"i.gtld-servers.net.": {"192.43.172.30"},
	"j.gtld-servers.net.": {"192.48.79.30"},
	"k.gtld-servers.net.": {"192.52.178.30"},
	"l.gtld-servers.net.": {"192.41.162.30"},
	"m.gtld-servers.net.": {"192.55.83.30"},
}

func NewDNSServer() *DNSServer {
	s := &DNSServer{
		client: &dns.Client{
			Timeout: queryTimeout,
		},
		cache:          make(map[string]*cacheEntry),
		nsCache:        make(map[string]*nsCacheEntry),
		badNameservers: make(map[string]time.Time),
		ipv6Available:  checkIPv6(),
	}
	if !s.ipv6Available {
		log.Println("IPv6 not available, will use IPv4 only")
	}
	return s
}

func checkIPv6() bool {
	conn, err := net.Dial("udp6", "[2001:4860:4860::8888]:53")
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (s *DNSServer) cacheKey(qname string, qtype uint16) string {
	return fmt.Sprintf("%s:%d", strings.ToLower(qname), qtype)
}

func (s *DNSServer) getFromCache(qname string, qtype uint16) *dns.Msg {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.cacheKey(qname, qtype)
	entry, ok := s.cache[key]
	if !ok {
		return nil
	}

	if time.Now().After(entry.expiration) {
		s.mu.RUnlock()
		s.mu.Lock()
		if current, ok := s.cache[key]; ok && time.Now().After(current.expiration) {
			delete(s.cache, key)
		}
		s.mu.Unlock()
		s.mu.RLock()
		return nil
	}

	return entry.response.Copy()
}

func (s *DNSServer) putInCache(qname string, qtype uint16, response *dns.Msg) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get the minimum TTL from the response
	ttl := s.getCacheTTL(response)
	if ttl < minCacheTTL {
		ttl = minCacheTTL
	} else if ttl > maxCacheTTL {
		ttl = maxCacheTTL
	}

	now := time.Now()
	key := s.cacheKey(qname, qtype)
	s.cache[key] = &cacheEntry{
		response:   response.Copy(),
		timestamp:  now,
		expiration: now.Add(ttl),
	}
}

func (s *DNSServer) getCacheTTL(response *dns.Msg) time.Duration {
	if response == nil {
		return minCacheTTL
	}

	if ttl, ok := getNegativeTTL(response); ok && isTerminalNegativeResponse(response) {
		return clampTTL(ttl)
	}

	minTTL := uint32(3600) // Default 1 hour
	found := false

	// Check Answer section
	for _, rr := range response.Answer {
		if rr.Header().Ttl < minTTL {
			minTTL = rr.Header().Ttl
			found = true
		}
	}

	// Check Authority section
	for _, rr := range response.Ns {
		if rr.Header().Ttl < minTTL {
			minTTL = rr.Header().Ttl
			found = true
		}
	}

	// Check Additional section
	for _, rr := range response.Extra {
		if rr.Header().Ttl < minTTL {
			minTTL = rr.Header().Ttl
			found = true
		}
	}

	if !found {
		return minCacheTTL
	}

	return clampTTL(time.Duration(minTTL) * time.Second)
}

func clampTTL(ttl time.Duration) time.Duration {
	if ttl < minCacheTTL {
		return minCacheTTL
	}
	if ttl > maxCacheTTL {
		return maxCacheTTL
	}
	return ttl
}

func getNegativeTTL(response *dns.Msg) (time.Duration, bool) {
	if response == nil {
		return 0, false
	}

	var minTTL uint32
	found := false

	sections := [][]dns.RR{response.Answer, response.Ns, response.Extra}
	for _, section := range sections {
		for _, rr := range section {
			soa, ok := rr.(*dns.SOA)
			if !ok {
				continue
			}
			ttl := soa.Hdr.Ttl
			if soa.Minttl < ttl {
				ttl = soa.Minttl
			}
			if !found || ttl < minTTL {
				minTTL = ttl
				found = true
			}
		}
	}

	if !found {
		return 0, false
	}

	return time.Duration(minTTL) * time.Second, true
}

func hasSOA(records []dns.RR) bool {
	for _, rr := range records {
		if _, ok := rr.(*dns.SOA); ok {
			return true
		}
	}
	return false
}

func hasNSReferral(records []dns.RR) bool {
	for _, rr := range records {
		if _, ok := rr.(*dns.NS); ok {
			return true
		}
	}
	return false
}

func isTerminalNegativeResponse(response *dns.Msg) bool {
	if response == nil {
		return false
	}
	if response.Rcode == dns.RcodeNameError {
		return true
	}
	return response.Rcode == dns.RcodeSuccess &&
		len(response.Answer) == 0 &&
		hasSOA(response.Ns) &&
		!hasNSReferral(response.Ns)
}

func getReferralZone(response *dns.Msg) string {
	for _, rr := range response.Ns {
		if ns, ok := rr.(*dns.NS); ok {
			return strings.ToLower(dns.Fqdn(ns.Hdr.Name))
		}
	}
	return ""
}

func getReferralTTL(response *dns.Msg) time.Duration {
	minTTL := uint32(3600)
	found := false

	for _, rr := range response.Ns {
		if rr.Header().Ttl < minTTL {
			minTTL = rr.Header().Ttl
			found = true
		}
	}
	for _, rr := range response.Extra {
		switch rr.(type) {
		case *dns.A, *dns.AAAA:
			if rr.Header().Ttl < minTTL {
				minTTL = rr.Header().Ttl
				found = true
			}
		}
	}

	if !found {
		return minCacheTTL
	}

	return clampTTL(time.Duration(minTTL) * time.Second)
}

func dedupeStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}

	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (s *DNSServer) getZoneNameservers(zone string) []string {
	if zone == "" {
		return nil
	}

	s.mu.RLock()
	entry, ok := s.nsCache[zone]
	s.mu.RUnlock()
	if !ok {
		return nil
	}

	if time.Now().After(entry.expiration) {
		s.mu.Lock()
		if current, ok := s.nsCache[zone]; ok && time.Now().After(current.expiration) {
			delete(s.nsCache, zone)
		}
		s.mu.Unlock()
		return nil
	}

	return append([]string(nil), entry.addresses...)
}

func (s *DNSServer) putZoneNameservers(zone string, addresses []string, ttl time.Duration) {
	if zone == "" || len(addresses) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.nsCache[zone] = &nsCacheEntry{
		addresses:  append([]string(nil), dedupeStrings(addresses)...),
		expiration: time.Now().Add(clampTTL(ttl)),
	}
}

func (s *DNSServer) isNameserverBad(ns string) bool {
	s.mu.RLock()
	expiration, ok := s.badNameservers[ns]
	s.mu.RUnlock()
	if !ok {
		return false
	}

	if time.Now().After(expiration) {
		s.mu.Lock()
		if current, ok := s.badNameservers[ns]; ok && time.Now().After(current) {
			delete(s.badNameservers, ns)
		}
		s.mu.Unlock()
		return false
	}

	return true
}

func (s *DNSServer) markNameserverBad(ns string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.badNameservers[ns] = time.Now().Add(badNameserverTTL)
}

func (s *DNSServer) clearNameserverBad(ns string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.badNameservers, ns)
}

// resolve performs recursive DNS resolution starting from root servers
func (s *DNSServer) resolve(qname string, qtype uint16, depth int) (*dns.Msg, error) {
	qc := &queryContext{inFlight: make(map[string]bool)}
	return s.resolveWithContext(qname, qtype, depth, qc)
}

func (s *DNSServer) resolveWithContext(qname string, qtype uint16, depth int, qc *queryContext) (*dns.Msg, error) {
	if depth > maxRecursionDepth {
		return nil, fmt.Errorf("maximum recursion depth exceeded")
	}

	// Check for circular dependency
	queryKey := fmt.Sprintf("%s:%d", strings.ToLower(qname), qtype)
	if qc.isInFlight(queryKey) {
		log.Printf("[Depth %d] Circular dependency detected for %s", depth, qname)
		return nil, fmt.Errorf("circular dependency detected")
	}

	// Check cache first
	if cached := s.getFromCache(qname, qtype); cached != nil {
		log.Printf("[Depth %d] Cache hit for %s %s", depth, qname, dns.TypeToString[qtype])
		return cached, nil
	}

	// Mark as in-flight
	qc.markInFlight(queryKey)
	defer qc.unmarkInFlight(queryKey)

	log.Printf("[Depth %d] Resolving %s %s", depth, qname, dns.TypeToString[qtype])

	// Start with root servers
	nameservers := rootServers

	// Iterate through the DNS hierarchy
	for {
		response, _, err := s.queryNameservers(qname, qtype, nameservers)
		if err != nil {
			return nil, err
		}

		if isTerminalNegativeResponse(response) {
			s.putInCache(qname, qtype, response)
			return response, nil
		}

		// If we got an answer, return it
		if len(response.Answer) > 0 {
			log.Printf("[Depth %d] Found answer for %s: %d records", depth, qname, len(response.Answer))
			s.putInCache(qname, qtype, response)
			return response, nil
		}

		// Check for CNAME in Answer section
		for _, rr := range response.Answer {
			if cname, ok := rr.(*dns.CNAME); ok {
				log.Printf("[Depth %d] Following CNAME: %s -> %s", depth, qname, cname.Target)
				return s.resolveWithContext(cname.Target, qtype, depth+1, qc)
			}
		}

		// Look for nameservers in Authority section
		var nextNS []string
		nsNames := make(map[string]bool)
		referralZone := getReferralZone(response)
		if cachedNS := s.getZoneNameservers(referralZone); len(cachedNS) > 0 {
			log.Printf("[Depth %d] Using cached nameservers for zone %s", depth, referralZone)
			nextNS = append(nextNS, cachedNS...)
		}

		for _, rr := range response.Ns {
			if ns, ok := rr.(*dns.NS); ok {
				nsNames[ns.Ns] = true
			}
		}

		// Try to get IPs from Additional section (prefer IPv4)
		if len(nextNS) == 0 {
			for nsName := range nsNames {
				found := false
				var ipv4Addrs []string
				var ipv6Addrs []string

				// Collect IPv4 and IPv6 addresses separately
				for _, rr := range response.Extra {
					if a, ok := rr.(*dns.A); ok && strings.EqualFold(a.Hdr.Name, nsName) {
						ipv4Addrs = append(ipv4Addrs, net.JoinHostPort(a.A.String(), "53"))
						found = true
					} else if aaaa, ok := rr.(*dns.AAAA); ok && strings.EqualFold(aaaa.Hdr.Name, nsName) {
						if s.ipv6Available {
							ipv6Addrs = append(ipv6Addrs, net.JoinHostPort(aaaa.AAAA.String(), "53"))
							found = true
						}
					}
				}

				// Add IPv4 first, then IPv6
				nextNS = append(nextNS, ipv4Addrs...)
				nextNS = append(nextNS, ipv6Addrs...)

				// If no glue records, resolve the nameserver
				if !found {
					log.Printf("[Depth %d] Resolving nameserver: %s", depth, nsName)

					// Check hints first for common TLD servers
					if ips, ok := tldHints[nsName]; ok {
						log.Printf("[Depth %d] Using hint for %s", depth, nsName)
						for _, ip := range ips {
							nextNS = append(nextNS, net.JoinHostPort(ip, "53"))
						}
						continue
					}

					nsResp, err := s.resolveWithContext(nsName, dns.TypeA, depth+1, qc)
					if err == nil && len(nsResp.Answer) > 0 {
						for _, rr := range nsResp.Answer {
							if a, ok := rr.(*dns.A); ok {
								nextNS = append(nextNS, net.JoinHostPort(a.A.String(), "53"))
							}
						}
					} else if err != nil {
						log.Printf("[Depth %d] Failed to resolve nameserver %s: %v", depth, nsName, err)
					}
				}
			}
		}
		nextNS = dedupeStrings(nextNS)

		if len(nextNS) == 0 {
			// No more nameservers to query
			if isTerminalNegativeResponse(response) {
				s.putInCache(qname, qtype, response)
				return response, nil // NXDOMAIN
			}
			return nil, fmt.Errorf("no nameservers found and no answer")
		}

		if referralZone != "" {
			s.putZoneNameservers(referralZone, nextNS, getReferralTTL(response))
		}
		log.Printf("[Depth %d] Following referral to %d nameserver(s)", depth, len(nextNS))
		nameservers = nextNS
	}
}

// queryNameservers queries a list of nameservers until one responds
func (s *DNSServer) queryNameservers(qname string, qtype uint16, nameservers []string) (*dns.Msg, string, error) {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(qname), qtype)
	m.RecursionDesired = false // We do the recursion

	for _, ns := range nameservers {
		// Skip IPv6 if not available
		if !s.ipv6Available && strings.Contains(ns, "[") {
			continue
		}
		if s.isNameserverBad(ns) {
			continue
		}

		response, _, err := s.client.Exchange(m, ns)
		if err == nil && response != nil {
			s.clearNameserverBad(ns)
			return response, ns, nil
		}
		if err == nil {
			continue
		}
		s.markNameserverBad(ns)
		// Only log non-IPv6 errors or if IPv6 is actually available
		if !strings.Contains(err.Error(), "network is unreachable") {
			log.Printf("Failed to query %s: %v", ns, err)
		}
	}

	return nil, "", fmt.Errorf("all nameservers failed")
}

// handleDNSRequest handles incoming DNS queries
func (s *DNSServer) handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	// Log the incoming query
	if len(r.Question) == 0 {
		response := new(dns.Msg)
		response.SetRcode(r, dns.RcodeFormatError)
		w.WriteMsg(response)
		return
	}

	q := r.Question[0]
	startTime := time.Now()
	log.Printf("Query: %s %s from %s", q.Name, dns.TypeToString[q.Qtype], w.RemoteAddr())

	if q.Qtype == dns.TypeANY {
		response := new(dns.Msg)
		response.SetRcode(r, dns.RcodeRefused)
		if err := w.WriteMsg(response); err != nil {
			if !strings.Contains(err.Error(), "broken pipe") && !strings.Contains(err.Error(), "connection reset") {
				log.Printf("Error writing response: %v", err)
			}
		} else {
			log.Printf("Rejected ANY query for %s from %s", q.Name, w.RemoteAddr())
			log.Printf("Response sent: %d answers, %d authority, %d additional (%.2fs)",
				len(response.Answer), len(response.Ns), len(response.Extra), time.Since(startTime).Seconds())
		}
		return
	}

	// Perform recursive resolution
	response, err := s.resolve(q.Name, q.Qtype, 0)

	duration := time.Since(startTime)
	if duration > 3*time.Second {
		log.Printf("Slow query warning: %s took %v", q.Name, duration)
	}

	if err != nil {
		log.Printf("Resolution failed: %v", err)
		response = new(dns.Msg)
		response.SetRcode(r, dns.RcodeServerFailure)
	}

	// Set the response ID to match the query
	response.SetReply(r)

	// Write the response back to the client
	if err := w.WriteMsg(response); err != nil {
		// Don't log "broken pipe" errors - they're normal when clients timeout
		if !strings.Contains(err.Error(), "broken pipe") && !strings.Contains(err.Error(), "connection reset") {
			log.Printf("Error writing response: %v", err)
		}
	} else {
		log.Printf("Response sent: %d answers, %d authority, %d additional (%.2fs)",
			len(response.Answer), len(response.Ns), len(response.Extra), duration.Seconds())
	}
}

func (s *DNSServer) Start(port string) error {
	// Create DNS server for UDP
	serverUDP := &dns.Server{
		Addr:    ":" + port,
		Net:     "udp",
		Handler: dns.HandlerFunc(s.handleDNSRequest),
	}

	// Create DNS server for TCP
	serverTCP := &dns.Server{
		Addr:    ":" + port,
		Net:     "tcp",
		Handler: dns.HandlerFunc(s.handleDNSRequest),
	}

	// Start UDP server in goroutine
	go func() {
		log.Printf("Starting DNS server on UDP port %s", port)
		if err := serverUDP.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start UDP server: %v", err)
		}
	}()

	// Start TCP server in goroutine
	go func() {
		log.Printf("Starting DNS server on TCP port %s", port)
		if err := serverTCP.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start TCP server: %v", err)
		}
	}()

	// Wait for shutdown signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("Shutting down DNS server...")
	serverUDP.Shutdown()
	serverTCP.Shutdown()

	return nil
}

func main() {
	port := defaultPort

	// Allow PORT environment variable to override
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = envPort
	} else if port == "53" {
		// Check if running as non-root and port is 53
		if os.Geteuid() != 0 {
			log.Println("Warning: Port 53 requires root privileges. Consider using a higher port (e.g., 5353) or run with sudo.")
			log.Println("Falling back to port 5353...")
			port = "5353"
		}
	}

	// Get local IP addresses
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		log.Println("Server will listen on:")
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					log.Printf("  - %s:%s", ipnet.IP.String(), port)
				}
			}
		}
	}

	log.Printf("Using root DNS servers for recursive resolution")
	log.Printf("Root servers: %d configured", len(rootServers))

	server := NewDNSServer()
	if err := server.Start(port); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
