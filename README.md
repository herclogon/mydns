# MyDNS - Recursive DNS Resolver Server

[![Go Version](https://img.shields.io/badge/go-1.16%2B-blue.svg)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

MyDNS is a lightweight recursive DNS resolver written in Go. It resolves records iteratively from the root servers instead of forwarding queries to upstream resolvers such as Google DNS or Cloudflare.

## Features

- True recursive resolution from the root zone down to authoritative nameservers
- UDP and TCP DNS serving
- In-memory caching with bounded TTLs
- Circular dependency detection for recursive nameserver lookups
- TLD hints for common gTLD nameservers
- Detailed query and resolution logging
- Automatic fallback from port `53` to `5353` when running as non-root
- Optional `PORT` override via environment variable

## Requirements

- Go 1.16 or newer
- Outbound DNS access to root and authoritative nameservers on UDP/TCP `53`

## Build

Build the native binary:

```bash
make binary
```

Build the Docker image:

```bash
make docker-build
```

`make build` remains available as an alias for `make docker-build`.
The Docker image uses a distroless runtime and listens on container port `53`.

## Run

Run as root on port `53`:

```bash
sudo ./mydns
```

Run as non-root. The binary will automatically fall back to `5353`:

```bash
./mydns
```

Run on a custom port:

```bash
PORT=8053 ./mydns
```

Build and run with Docker on host port `5353`:

```bash
make docker-build
make run
```

Run with Docker on host port `53`:

```bash
make docker-build
make run-privileged
```

## Test

Query port `53`:

```bash
dig @127.0.0.1 example.com
```

Query port `5353`:

```bash
dig @127.0.0.1 -p 5353 example.com
```

## Deployment

Deployment guidance now lives in [DEPLOYMENT.md](DEPLOYMENT.md). It covers:

- native and systemd deployments
- Docker deployment
- exposing public DNS on `53`
- using DNAT from `53` to `5353`
- interaction with other local DNS listeners such as `dnsmasq`
- operational checks and troubleshooting

## Technical Notes

- Recursive resolution depth is capped at `15`
- Per-query upstream timeout is `5s`
- Cache TTLs are bounded to `10s` minimum and `1h` maximum
- NXDOMAIN and authoritative NODATA responses are cached using SOA-derived negative TTLs when available
- Recently failing upstream nameservers are temporarily avoided
- Delegated nameserver addresses are cached per zone to reduce repeated NS host lookups
- IPv6 upstream queries are only used when IPv6 appears available locally
- The server listens on `:<port>` for both UDP and TCP, which means it attempts to bind all local IPv4 addresses on that port

## Make Targets

- `make binary`
- `make build`
- `make docker-build`
- `make run`
- `make run-privileged`
- `make logs`
- `make stop`
- `make clean`
- `make test`
- `make help`

## Disclaimer

This is an educational and experimental DNS server. For production-critical use, evaluate established resolvers such as Unbound, PowerDNS, or BIND.

## Known Limitations

- No DNSSEC validation
- IPv6 nameserver resolution is basic
- GeoDNS answers may differ from public resolvers because resolution originates from your host
- `ANY` queries are refused to reduce abuse and unnecessary recursion

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE).
