# Research Notes

This document provides context on related work, design decisions, and research directions for Peer Compute.

## Related Work

### Peer-to-Peer Compute Platforms

| System | Year | Key Contribution | Difference from Peer Compute |
|--------|------|------------------|------------------------------|
| BOINC | 2002 | Volunteer computing | Fixed task model, no containers |
| Golem | 2016 | Decentralized compute marketplace | Blockchain-based, complex |
| iExec | 2017 | Trusted execution | SGX dependency |
| Akash | 2020 | Container deployment | Kubernetes-native, blockchain |

### P2P Networking

| Protocol | Use in Peer Compute | Rationale |
|----------|---------------------|-----------|
| libp2p | Core networking | Modular, well-maintained, IPFS-proven |
| Noise | Stream encryption | Forward secrecy, mutual auth |
| Kademlia | Future: global discovery | Efficient DHT implementation |

### Container Security

| Technique | Implemented | Status |
|-----------|-------------|--------|
| Namespaces | Yes | Linux kernel isolation |
| Cgroups v2 | Yes | Resource limiting |
| Seccomp | Yes | Syscall filtering |
| AppArmor | Planned | MAC enforcement |
| gVisor | Considered | User-space kernel |
| Kata | Considered | VM-based isolation |

## Design Decisions

### D1: Why Ed25519 over RSA/ECDSA?

**Decision**: Use Ed25519 for all cryptographic identities

**Rationale**:
1. **Performance**: 10x faster than RSA-2048 signing
2. **Security**: 128-bit security level, no known weaknesses
3. **Key size**: 32-byte keys vs 256+ for RSA
4. **Determinism**: No random number needed for signing
5. **Compatibility**: Supported by libp2p natively

### D2: Why Noise over TLS?

**Decision**: Use Noise protocol for P2P encryption

**Rationale**:
1. **Mutual authentication**: Both sides authenticate
2. **No PKI required**: No certificate authorities
3. **Forward secrecy**: Ephemeral keys per session
4. **Simplicity**: Smaller attack surface than TLS

### D3: Why Explicit Trust over Web-of-Trust?

**Decision**: Require explicit peer-by-peer trust

**Rationale**:
1. **Auditability**: Clear who can deploy
2. **Simplicity**: No trust delegation complexity
3. **Control**: Users decide exactly who to trust
4. **Security**: No transitive trust vulnerabilities

Trade-off: Requires manual trust establishment

### D4: Why Reverse Tunnels over Direct Connections?

**Decision**: Providers initiate outbound tunnels to gateway

**Rationale**:
1. **NAT traversal**: Works behind any firewall
2. **No port forwarding**: No router configuration
3. **Simplified security**: Only outbound connections
4. **Centralized routing**: Gateway handles TLS termination

Trade-off: Dependency on gateway availability

### D5: Why JSON over Protocol Buffers?

**Decision**: Use JSON for protocol messages (with protobuf option)

**Rationale**:
1. **Debuggability**: Human-readable messages
2. **Flexibility**: Easy to extend
3. **Ecosystem**: Universal language support

Trade-off: Slightly larger message size (acceptable for control plane)

## Performance Considerations

### Baseline Measurements (Target)

| Operation | Target Latency | Notes |
|-----------|---------------|-------|
| Peer connection | < 500ms | Over LAN |
| Deploy request | < 1s | Plus image pull |
| Image pull | Network-bound | Docker cache helps |
| Container start | < 2s | Typical Docker startup |
| Log streaming | < 100ms delay | Real-time streaming |
| Tunnel latency | < 50ms overhead | TCP forwarding |

### Scalability

| Resource | Single Provider Limit | Notes |
|----------|----------------------|-------|
| Concurrent containers | 50 | Memory-limited in practice |
| P2P connections | 400 | libp2p connection manager |
| Tunnel multiplexing | 1000 streams | QUIC-based |

## Future Research Directions

### R1: Trusted Execution Environments

Explore hardware-based isolation:
- Intel SGX enclaves
- AMD SEV for VM isolation
- ARM TrustZone

Challenge: Provider must have compatible hardware

### R2: Decentralized Gateway

Remove single point of failure:
- DHT-based routing
- Anycast DNS
- Edge caching

### R3: Resource Pricing

Economic incentives for providers:
- Spot pricing for compute
- Payment channels (Lightning-style)
- Collateralization for SLAs

### R4: Privacy-Preserving Deployment

Hide deployment details from providers:
- Encrypted container images
- Secure multi-party computation
- Homomorphic encryption (long-term)

### R5: Attestation

Prove container execution integrity:
- TPM-based attestation
- Container image signing
- Execution proofs

## Academic Context

### Potential Research Questions

1. How does explicit trust affect network formation?
2. What is the optimal tunnel topology for latency minimization?
3. How can we verify correct container execution?
4. What economic models encourage honest participation?

### Relevant Conferences

- USENIX Security
- ACM CCS
- NDSS
- IEEE S&P
- OSDI/SOSP (systems)
- EuroSys

## Implementation Notes

### Code Quality Guidelines

1. **Security-critical paths**: Extensive comments explaining rationale
2. **Error handling**: Wrap all errors with context
3. **Logging**: Security events logged for audit
4. **Testing**: Property-based tests for protocol parsing

### Audit Focus Areas

Priority for security review:
1. `internal/identity/` - Key management
2. `internal/p2p/gater.go` - Connection gating
3. `internal/runtime/docker.go` - Container execution
4. `internal/protocol/codec.go` - Message parsing
5. `internal/security/` - Signing/verification

## References

1. Maymounkov, P., & Mazières, D. (2002). Kademlia: A Peer-to-peer Information System Based on the XOR Metric.

2. Perrig, A., et al. (2017). SCION: A Secure Internet Architecture.

3. Sultan, S., Ahmad, I., & Dimitriou, T. (2019). Container Security: Issues, Challenges, and the Road Ahead.

4. Kocher, P., et al. (2019). Spectre Attacks: Exploiting Speculative Execution.

5. libp2p Specification: https://github.com/libp2p/specs

6. Noise Protocol Framework: https://noiseprotocol.org/noise.html

7. Docker Security Documentation: https://docs.docker.com/engine/security/
