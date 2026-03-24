# Deploy Helpers

This directory contains deployment artifacts for `mydns`.

## Files

- `mydns.service`: systemd unit for running `mydns` as user `mydns`
- `setup-iptables-dnat.sh`: configures public `53 -> 5353` DNAT with `iptables`
- `logrotate-mydns`: sample logrotate rule for `/var/log/mydns.log`

## Systemd

Install the service:

```bash
sudo cp deploy/mydns.service /etc/systemd/system/mydns.service
sudo systemctl daemon-reload
sudo systemctl enable --now mydns
```

This unit writes both stdout and stderr to `/var/log/mydns.log`.

Prepare the log file:

```bash
sudo touch /var/log/mydns.log
sudo chown mydns:mydns /var/log/mydns.log
sudo chmod 0644 /var/log/mydns.log
```

## DNAT

Expose public DNS on `53` while `mydns` listens on `5353`:

```bash
sudo bash deploy/setup-iptables-dnat.sh <public-ip> [public-interface]
```

Example:

```bash
sudo bash deploy/setup-iptables-dnat.sh 178.208.86.121 eth0
```

If `netfilter-persistent` is installed, the script also saves the rules.

## Logrotate

Install the sample logrotate config:

```bash
sudo cp deploy/logrotate-mydns /etc/logrotate.d/mydns
sudo logrotate -d /etc/logrotate.d/mydns
```
