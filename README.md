# MyDNS - Recursive DNS Resolver Server

[![Go Version](https://img.shields.io/badge/go-1.16%2B-blue.svg)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A lightweight DNS server written in Go that performs recursive DNS resolution starting from root DNS servers, just like a real DNS resolver.

## Overview

MyDNS is a recursive DNS resolver that implements the full DNS resolution process from scratch. Unlike forwarding DNS servers that simply relay queries to upstream resolvers (like Google DNS or Cloudflare), MyDNS queries the DNS infrastructure directly, starting from the root servers and following referrals through the DNS hierarchy to find authoritative answers.

## Features

- **True recursive resolution** - starts from root servers and follows the DNS hierarchy
- Supports both UDP and TCP protocols
- Handles CNAME resolution and DNS referrals
- Resolves nameserver addresses when needed (glue records)
- Detailed logging of the resolution process
- Automatic port selection (53 with root, 5353 without)

## Requirements

- Go 1.16 or higher

## Installation

### Build from source:
```bash
go build -o mydns
```

### Using Docker:
```bash
make build
```

## Usage

### Native Binary

#### Run with root privileges (port 53):
```bash
sudo ./mydns
```

#### Run without root privileges (port 5353):
```bash
./mydns
```

### Docker Container

#### Quick start (port 5353):
```bash
make run
```

#### Run on port 53 (privileged):
```bash
make run-privileged
```

#### View logs:
```bash
make logs
```

#### Stop server:
```bash
make stop
```

#### See all commands:
```bash
make help
```

## Testing

Test the DNS server using `dig` or `nslookup`:

### Using dig (port 53):
```bash
dig @localhost example.com
```

### Using dig (port 5353):
```bash
dig @localhost -p 5353 example.com
```

### Using nslookup (port 53):
```bash
nslookup example.com localhost
```

### Using nslookup (port 5353):
```bash
nslookup -port=5353 example.com localhost
```

## How It Works

1. The server listens for DNS queries on the specified port (both UDP and TCP)
2. When a query is received, it starts recursive resolution from the root DNS servers
3. It follows DNS referrals through the hierarchy (root → TLD → authoritative)
4. It resolves nameserver addresses when glue records are not provided
5. It handles CNAME records by following them to the final answer
6. The complete answer is returned to the client

## Stop the Server

Press `Ctrl+C` to gracefully shutdown the server.

## Example Resolution Process

When you query `example.com`, MyDNS:
1. Queries a root server for `.com` nameservers
2. Queries a `.com` TLD server for `example.com` nameservers
3. Queries the authoritative nameserver for `example.com`
4. Returns the final answer to you

All of this happens transparently with detailed logging.

## Technical Details

- Written in Go using the [miekg/dns](https://github.com/miekg/dns) library
- Implements iterative DNS resolution with recursion handling
- Supports A, AAAA, CNAME, NS, MX, TXT, and other DNS record types
- Handles glue records and nameserver resolution
- Maximum recursion depth: 15 levels
- Query timeout: 5 seconds per query
- Docker image based on distroless for minimal attack surface (~2MB)

## Docker

The project includes a multi-stage Dockerfile that creates a minimal, secure container:

- **Build stage**: Compiles the Go binary
- **Runtime stage**: Uses Google's distroless image (no shell, minimal dependencies)
- **Size**: ~2MB final image
- **Security**: Runs as non-root user

Available make commands:
- `make build` - Build Docker image
- `make run` - Run on port 5353
- `make run-privileged` - Run on port 53
- `make logs` - View container logs
- `make stop` - Stop and remove container
- `make clean` - Remove container and image
- `make test` - Test DNS resolution
- `make help` - Show all commands

## Contributing

Contributions are welcome! Feel free to open issues or submit pull requests.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Disclaimer

This is an educational/experimental DNS server. For production use, consider established solutions like BIND, Unbound, or PowerDNS.
