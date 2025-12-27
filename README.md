# Peer Compute

> Decentralized peer-to-peer compute platform for deploying Docker containers on trusted peer machines.

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

## Overview

Peer Compute enables developers to deploy Docker containers on trusted peer machines and securely expose them via reverse tunnels. The system works behind NAT and firewalls, requires no inbound ports on provider machines, and uses explicit trust between peers.

```
┌─────────────┐     P2P Network      ┌─────────────────┐
│   peerctl   │◄────────────────────►│   peercomputed  │
│    (CLI)    │    Signed Requests   │    (Provider)   │
└─────────────┘                      └────────┬────────┘
                                              │
                                     Reverse Tunnel
                                              │
                                     ┌────────▼────────┐
                                     │     Gateway     │
                                     │ (Public Access) │
                                     └────────┬────────┘
                                              │
                                        HTTPS │
                                              │
                                     ┌────────▼────────┐
                                     │   End Users     │
                                     └─────────────────┘
```

## Features

- **Zero-Trust Networking**: Explicit peer approval with mutual authentication
- **NAT Traversal**: Works behind firewalls via outbound reverse tunnels
- **Container Isolation**: Strict resource limits, no host mounts, non-privileged
- **Secure by Default**: Ed25519 cryptographic identities, Noise encryption
- **Simple CLI**: Deploy containers with a single command
- **Public Gateway**: Expose containers via subdomain-based routing

## Quick Start

### Prerequisites

- Go 1.22 or later
- Docker installed and running

### Installation

```bash
# Clone the repository
git clone https://github.com/xdas-research/peer-compute.git
cd peer-compute

# Build all binaries
go build -o bin/peerctl ./cmd/peerctl
go build -o bin/peercomputed ./cmd/peercomputed
go build -o bin/gateway ./cmd/gateway
```

### Initialize Your Identity

```bash
# Generate your cryptographic identity
./bin/peerctl init

# Output:
# ✓ Generated new identity
# Your Peer ID: 12D3KooWRq3bMEaFjZ...
```

### Add Trusted Peers

Exchange peer IDs out-of-band (Signal, email, etc.) and add them:

```bash
./bin/peerctl peers add 12D3KooW... --name "alice" --addr "/ip4/192.168.1.100/tcp/9000"
```

### Run the Provider Daemon

On machines providing compute resources:

```bash
# Without gateway (local access only)
./bin/peercomputed --port 9000

# With gateway (public access)
./bin/peercomputed --port 9000 --gateway your-gateway.com:8443
```

### Deploy Containers

```bash
# Deploy nginx to a peer
./bin/peerctl deploy nginx:alpine --peer alice --cpu 0.5 --memory 256M --expose 80

# View logs
./bin/peerctl logs dep-123456789

# Stop the deployment
./bin/peerctl stop dep-123456789
```

## CLI Reference

### `peerctl init`

Initialize a new cryptographic identity.

```bash
peerctl init [--force]
```

### `peerctl peers`

Manage trusted peers.

```bash
peerctl peers add <peer-id> [--name NAME] [--addr MULTIADDR]
peerctl peers remove <peer-id>
peerctl peers list
```

### `peerctl deploy`

Deploy a container to a peer.

```bash
peerctl deploy <image> --peer <peer> [options]

Options:
  --cpu         CPU limit (e.g., 0.5, 1, 2)
  --memory      Memory limit (e.g., 256M, 1G)
  --expose      Container port to expose
  --env         Environment variables (KEY=VALUE)
  --timeout     Deployment timeout
```

### `peerctl logs`

Stream logs from a deployment.

```bash
peerctl logs <deployment-id> [--follow] [--tail N]
```

### `peerctl stop`

Stop a deployment.

```bash
peerctl stop <deployment-id> [--force]
```

## Architecture

See [docs/architecture.md](docs/architecture.md) for detailed system design.

## Security

Peer Compute is designed with security as a first-class concern:

| Control | Implementation |
|---------|----------------|
| Identity | Ed25519 cryptographic keys |
| Encryption | Noise protocol (ChaCha20-Poly1305) |
| Authentication | Mutual authentication via peer IDs |
| Authorization | Explicit allow-listing |
| Container Isolation | No host mounts, non-privileged, seccomp |
| Resource Limits | Strict CPU/memory cgroups |
| Network | Containers bind to localhost only |

See [docs/threat-model.md](docs/threat-model.md) for the threat model.

## Gateway Setup

For public access to deployed containers:

```bash
./bin/gateway \
  --domain peercompute.example.com \
  --acme-email admin@example.com \
  --https-port 443 \
  --tunnel-port 8443
```

The gateway:
- Terminates TLS using Let's Encrypt
- Assigns subdomains per deployment
- Routes traffic through reverse tunnels
- Never runs user containers

## Project Structure

```
peer-compute/
├── cmd/
│   ├── peerctl/          # Developer CLI
│   ├── peercomputed/     # Provider daemon
│   └── gateway/          # Public ingress gateway
├── internal/
│   ├── identity/         # Cryptographic identity
│   ├── p2p/              # libp2p networking
│   ├── protocol/         # Message types
│   ├── runtime/          # Docker execution
│   ├── scheduler/        # Deployment management
│   ├── handler/          # P2P request handlers
│   ├── client/           # P2P client
│   └── tunnel/           # Reverse tunnels
├── examples/
│   └── express-hello/    # Example Express.js app
├── docs/
│   ├── architecture.md
│   ├── threat-model.md
│   └── research-notes.md
└── README.md
```

## Research Context

Peer Compute is developed by [XDAS Research](https://github.com/xdas-research) as an open-source platform suitable for both academic research and real-world deployment. The design prioritizes:

1. **Verifiable Security**: All security-critical code paths are extensively commented
2. **Reproducibility**: Deterministic builds and well-defined protocols
3. **Extensibility**: Modular architecture for experimentation

## Contributing

Contributions are welcome! Please read our contributing guidelines before submitting PRs.

## License

Peer Compute is an open-source project licensed under **Apache License 2.0**. See [LICENSE](LICENSE) for details.

## Acknowledgments

- [libp2p](https://libp2p.io/) for P2P networking
- [Docker](https://www.docker.com/) for container runtime
- [Cobra](https://cobra.dev/) for CLI framework
