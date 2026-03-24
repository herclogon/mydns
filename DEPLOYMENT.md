# Deployment Guide

This document describes how to deploy `mydns` in practice, including the constraints of the current binary.

## Current Runtime Behavior

`mydns` serves both UDP and TCP on `:<port>`.

- If `PORT` is unset and the process runs as `root`, it uses port `53`
- If `PORT` is unset and the process runs as non-root, it logs a warning and falls back to `5353`
- If `PORT` is set, `mydns` uses that value directly

Important operational detail:

- The current binary binds `:<port>`, not a specific IP
- In practice that means it attempts to listen on all local IPv4 addresses for both UDP and TCP
- If another service already owns `53` on any local address, `mydns` can fail to start on `53`

This matters on hosts that already run local DNS infrastructure such as LXD `dnsmasq` on `10.102.66.1:53`.

## Deployment Patterns

### 1. Direct bind on `53`

Use this when the host does not already have another DNS service bound to port `53`.

Build:

```bash
cd ~/Projects/pets/mydns
go build -o mydns .
```

Run:

```bash
sudo ./mydns
```

Verify:

```bash
ss -luntp | grep ':53'
dig @127.0.0.1 example.com
dig +tcp @127.0.0.1 example.com
```

### 2. Non-root service on `5353` with public `53 -> 5353` DNAT

Use this when:

- you want to keep `mydns` unprivileged
- another service already occupies `53` on part of the host
- you only need public traffic on `eth0:53` forwarded to `mydns`

This is the safest deployment pattern for the current codebase on multi-purpose hosts.

High-level flow:

```text
public client -> host:53 -> DNAT -> host:5353 -> mydns
```

### 3. Docker on `5353`

Use this for local testing or when another fronting layer will publish the service.

```bash
make build
make run
dig @127.0.0.1 -p 5353 example.com
```

## Recommended Systemd Deployment

For the current host layout, prefer `mydns` as a dedicated unprivileged user on `5353`, fronted by firewall DNAT from public `53`.

### Create User

```bash
sudo useradd --system --home /opt/mydns --shell /usr/sbin/nologin mydns
```

If the user already exists, keep it.

### Install Binary

```bash
sudo mkdir -p /opt/mydns
sudo cp ./mydns /opt/mydns/mydns
sudo chown -R mydns:mydns /opt/mydns
sudo chmod 0755 /opt/mydns/mydns
```

### Systemd Unit

The repository includes a ready-to-copy unit file at `deploy/mydns.service`.

Copy it into place:

```bash
sudo cp deploy/mydns.service /etc/systemd/system/mydns.service
```

Its contents are:

```ini
[Unit]
Description=mydns recursive resolver
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=mydns
Group=mydns
WorkingDirectory=/opt/mydns
ExecStart=/opt/mydns/mydns
Restart=always
RestartSec=5
StandardOutput=append:/var/log/mydns.log
StandardError=append:/var/log/mydns.log

[Install]
WantedBy=multi-user.target
```

Prepare the log file before starting the service:

```bash
sudo touch /var/log/mydns.log
sudo chown mydns:mydns /var/log/mydns.log
sudo chmod 0644 /var/log/mydns.log
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now mydns
```

Verify:

```bash
systemctl status mydns --no-pager
ss -luntp | grep ':5353'
tail -n 50 /var/log/mydns.log
dig @127.0.0.1 -p 5353 example.com
dig +tcp @127.0.0.1 -p 5353 example.com
```

### Log Rotation

The repository includes a sample logrotate rule at `deploy/logrotate-mydns`.

Install it:

```bash
sudo cp deploy/logrotate-mydns /etc/logrotate.d/mydns
sudo logrotate -d /etc/logrotate.d/mydns
```

The sample policy:

- rotates weekly
- keeps 8 compressed archives
- uses `copytruncate` so the running process can continue writing without a restart

## Publish Public DNS on Port `53`

If `mydns` is running on `5353`, expose it publicly with DNAT on the public interface.

The repository includes a helper script at `deploy/setup-iptables-dnat.sh`.

Example:

```bash
sudo bash deploy/setup-iptables-dnat.sh PUBLIC_IP eth0
```

Example with `iptables`:

```bash
sudo iptables -t nat -A PREROUTING -i eth0 -p udp --dport 53 -j DNAT --to-destination PUBLIC_IP:5353
sudo iptables -t nat -A PREROUTING -i eth0 -p tcp --dport 53 -j DNAT --to-destination PUBLIC_IP:5353
```

Replace `PUBLIC_IP` with the host's public IPv4 address.

If you persist rules with `netfilter-persistent`:

```bash
sudo netfilter-persistent save
```

Verify counters and behavior:

```bash
sudo iptables -t nat -L PREROUTING -n -v
dig @PUBLIC_IP example.com
dig +tcp @PUBLIC_IP example.com
```

## Docker Deployment

The image runs as a non-root user by default.

Build:

```bash
make build
```

Run on host port `5353`:

```bash
make run
```

Run on host port `53`:

```bash
make run-privileged
```

Notes:

- `--cap-add=NET_BIND_SERVICE` is necessary for privileged port binding inside the container
- direct port `53` publishing can still conflict with another service already using `53` on the host
- if the host already uses `53`, publish the container on `5353` and front it with DNAT

## Troubleshooting

### Service is restarting with `bind: address already in use`

Check listeners:

```bash
ss -luntp | grep -E '(:53|:5353)'
```

Common causes:

- a stale manually started `mydns` process is still running
- another resolver such as `dnsmasq`, `systemd-resolved`, or a container runtime service owns the port
- you attempted direct bind on `53` while LXD `dnsmasq` was already bound to `10.102.66.1:53`

Check service logs:

```bash
journalctl -u mydns --no-pager -n 100
tail -n 100 /var/log/mydns.log
```

### Public `53` times out but local `5353` works

Check:

- DNAT rules exist for both UDP and TCP
- firewall policy allows inbound traffic to public `53`
- `mydns` is actually listening on `5353`
- packets are hitting the NAT counters

Useful commands:

```bash
sudo iptables -t nat -L PREROUTING -n -v
ss -luntp | grep ':5353'
dig @127.0.0.1 -p 5353 example.com
dig @PUBLIC_IP example.com
```

### Direct bind on `53` fails even as root

This usually means another service owns `53` on at least one local address.

Because the current `mydns` binary binds wildcard addresses, it cannot currently be told to bind only the public IP. If that is required, you need one of these:

- modify `mydns` to bind a configurable listen address
- remove or reconfigure the competing local DNS service
- keep `mydns` on `5353` and use DNAT from public `53`

## Production Notes

- Ensure outbound UDP/TCP `53` is allowed from the host; `mydns` resolves iteratively against root, TLD, and authoritative servers
- Monitor logs for repeated upstream timeouts
- Use a process supervisor such as systemd
- Treat this project as experimental software

## Suggested Future Improvements

- configurable listen address, not only `:<port>`
- explicit config file support
- health endpoint or built-in readiness probe
- better shutdown handling for long-running upstream lookups
- negative caching and DNSSEC support
