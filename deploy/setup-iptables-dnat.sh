#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID} -ne 0 ]]; then
  echo "Run as root." >&2
  exit 1
fi

PUBLIC_IP="${1:-}"
PUBLIC_IFACE="${2:-eth0}"

if [[ -z "${PUBLIC_IP}" ]]; then
  echo "Usage: $0 <public-ip> [public-interface]" >&2
  echo "Example: $0 178.208.86.121 eth0" >&2
  exit 1
fi

UDP_RULE=(-i "${PUBLIC_IFACE}" -p udp --dport 53 -j DNAT --to-destination "${PUBLIC_IP}:5353")
TCP_RULE=(-i "${PUBLIC_IFACE}" -p tcp --dport 53 -j DNAT --to-destination "${PUBLIC_IP}:5353")

iptables -t nat -C PREROUTING "${UDP_RULE[@]}" 2>/dev/null || iptables -t nat -A PREROUTING "${UDP_RULE[@]}"
iptables -t nat -C PREROUTING "${TCP_RULE[@]}" 2>/dev/null || iptables -t nat -A PREROUTING "${TCP_RULE[@]}"

if command -v netfilter-persistent >/dev/null 2>&1; then
  netfilter-persistent save
fi

echo "Configured DNAT for ${PUBLIC_IFACE} ${PUBLIC_IP}:53 -> ${PUBLIC_IP}:5353"
iptables -t nat -S PREROUTING | grep -- '--dport 53' || true
