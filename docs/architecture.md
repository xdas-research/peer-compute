# Peer Compute Architecture

## System Overview

Peer Compute is a decentralized platform that enables deployment of Docker containers on trusted peer machines. The system uses P2P networking for coordination and reverse tunnels for public access.

## Core Components

### 1. Identity Layer

Every participant in the network has a cryptographic identity:

```
┌─────────────────────────────────────────┐
│              Identity                    │
├─────────────────────────────────────────┤
│  Private Key (Ed25519)                  │
│  ├─ Stored locally                      │
│  ├─ Never transmitted                   │
│  └─ Used for signing requests           │
│                                         │
│  Public Key                             │
│  ├─ Shared with peers                   │
│  └─ Used to derive Peer ID              │
│                                         │
│  Peer ID (Multihash of Public Key)      │
│  └─ Public identifier in P2P network    │
└─────────────────────────────────────────┘
```

### 2. P2P Networking Layer

Built on libp2p with the following configuration:

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Transport | TCP | Reliable, widely supported |
| Security | Noise | Forward secrecy, mutual auth |
| Multiplexing | Yamux | Efficient stream multiplexing |
| Discovery | mDNS | Local network discovery |

### 3. Trust Model

```
     Alice                    Bob
       │                       │
       │  Exchange Peer IDs    │
       │◄─────────────────────►│
       │   (out-of-band)       │
       │                       │
       ▼                       ▼
  ┌─────────┐             ┌─────────┐
  │ peerctl │             │ peerctl │
  │ peers   │             │ peers   │
  │ add Bob │             │ add     │
  │         │             │ Alice   │
  └─────────┘             └─────────┘
       │                       │
       └───► Mutual Trust ◄────┘
```

Trust is:
- **Explicit**: Peers must be manually added
- **Mutual**: Both sides must add each other
- **Persistent**: Stored in `~/.peercompute/trusted_peers.json`

### 4. Protocol Messages

All messages are length-prefixed JSON with type headers:

```
┌──────────┬──────────────┬─────────────────────┐
│ Type (1B)│ Length (4B)  │ JSON Payload        │
└──────────┴──────────────┴─────────────────────┘
```

#### Message Types

| Type | Description |
|------|-------------|
| DeployRequest | Request to deploy a container |
| DeployResponse | Deployment result with URL |
| StopRequest | Request to stop a deployment |
| StopResponse | Stop confirmation |
| LogEntry | Container log message |
| StatusRequest | Query deployment status |
| StatusResponse | Deployment status info |

### 5. Container Runtime

Containers run with strict isolation:

```
┌─────────────────────────────────────────────────────┐
│                  Docker Container                    │
│  ┌───────────────────────────────────────────────┐  │
│  │  User Application                             │  │
│  └───────────────────────────────────────────────┘  │
│                                                     │
│  Security Constraints:                              │
│  • Non-privileged                                   │
│  • No host mounts                                   │
│  • Dropped capabilities (all)                       │
│  • Seccomp profile (default)                        │
│  • CPU cgroups limit                                │
│  • Memory cgroups limit                             │
│  • Network: bridge (localhost only)                 │
│  • no-new-privileges                                │
└─────────────────────────────────────────────────────┘
```

### 6. Reverse Tunnel Architecture

```
┌──────────────┐                    ┌──────────────┐
│   Provider   │                    │   Gateway    │
│              │                    │              │
│  Container   │                    │  HTTPS       │
│  (port 8080) │                    │  (port 443)  │
│      ▲       │                    │      ▲       │
│      │       │                    │      │       │
│  localhost   │                    │   Internet   │
│      ▲       │                    │              │
│      │       │   Outbound TLS     │      │       │
│  Tunnel  ────┼────────────────────┼──►Tunnel     │
│  Client      │    Connection      │   Server     │
└──────────────┘                    └──────────────┘
```

Key properties:
- **Outbound only**: Provider initiates connection
- **Encrypted**: TLS 1.3 for tunnel traffic
- **Multiplexed**: Single connection for all deployments
- **Resilient**: Auto-reconnect on disconnection

## Data Flow

### Deployment Flow

```
┌─────────┐    ┌───────────┐    ┌─────────────┐    ┌─────────┐
│ peerctl │    │  P2P Net  │    │ peercomputed│    │ Docker  │
└────┬────┘    └─────┬─────┘    └──────┬──────┘    └────┬────┘
     │               │                 │                │
     │ Deploy Request│                 │                │
     │──────────────►│                 │                │
     │               │ Forward         │                │
     │               │────────────────►│                │
     │               │                 │                │
     │               │                 │ Verify Trust   │
     │               │                 │────────────────│
     │               │                 │                │
     │               │                 │ Pull Image     │
     │               │                 │───────────────►│
     │               │                 │◄───────────────│
     │               │                 │                │
     │               │                 │ Create Container
     │               │                 │───────────────►│
     │               │                 │◄───────────────│
     │               │                 │                │
     │               │   Response      │                │
     │◄──────────────│◄────────────────│                │
     │               │                 │                │
```

### Traffic Flow (with Gateway)

```
┌──────────┐    ┌─────────┐    ┌────────┐    ┌───────────┐
│  Client  │    │ Gateway │    │ Tunnel │    │ Container │
└────┬─────┘    └────┬────┘    └───┬────┘    └─────┬─────┘
     │               │             │               │
     │  HTTPS        │             │               │
     │──────────────►│             │               │
     │               │ Route by    │               │
     │               │ subdomain   │               │
     │               │────────────►│               │
     │               │             │ Forward       │
     │               │             │──────────────►│
     │               │             │◄──────────────│
     │               │◄────────────│               │
     │◄──────────────│             │               │
     │               │             │               │
```

## Configuration

### Provider (peercomputed)

| Config | Default | Description |
|--------|---------|-------------|
| port | 9000 | P2P listen port |
| max-cpu | 4000 | Max CPU in millicores |
| max-memory | 4GB | Max memory allocation |
| max-deploys | 10 | Max concurrent containers |
| gateway | - | Gateway address for tunnels |

### Gateway

| Config | Default | Description |
|--------|---------|-------------|
| domain | peercompute.xdastechnology.com | Base domain |
| https-port | 443 | HTTPS port |
| tunnel-port | 8443 | Tunnel listen port |
| rate-limit | 100 | Requests per second |

## Security Boundaries

```
┌─────────────────────────────────────────────────────────────────┐
│                        Trust Boundary 1                          │
│  ┌─────────────┐                        ┌─────────────────────┐ │
│  │   Peer A    │◄──── P2P (Noise) ────►│      Peer B         │ │
│  │  (Trusted)  │                        │     (Trusted)       │ │
│  └─────────────┘                        └─────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
                              │
                     Tunnel (TLS)
                              │
┌─────────────────────────────▼───────────────────────────────────┐
│                        Trust Boundary 2                          │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                        Gateway                               ││
│  │   • Does NOT run containers                                  ││
│  │   • Routes traffic only                                      ││
│  │   • Rate limited                                             ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
                              │
                     HTTPS (TLS)
                              │
┌─────────────────────────────▼───────────────────────────────────┐
│                        Trust Boundary 3                          │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                     Public Internet                          ││
│  │   • Untrusted                                                ││
│  │   • All input validated                                      ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

## Future Extensions

The modular architecture supports:

1. **Kubernetes Support**: Replace Docker runtime with containerd
2. **DHT Discovery**: Global peer discovery via Kademlia DHT
3. **Multi-tenancy**: Per-organization trust namespaces
4. **Billing**: Resource accounting and payment channels
5. **Auto-scheduling**: Automatic placement based on resources
