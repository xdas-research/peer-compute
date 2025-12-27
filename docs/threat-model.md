# Threat Model

This document describes the security assumptions, threat actors, attack vectors, and mitigations for Peer Compute.

## Security Goals

1. **Confidentiality**: Communications between peers are encrypted
2. **Integrity**: Requests cannot be tampered with or forged
3. **Authentication**: Only trusted peers can communicate
4. **Authorization**: Only authorized peers can deploy containers
5. **Isolation**: Containers cannot escape or affect the host
6. **Availability**: System remains functional under attack

## Trust Assumptions

### What We Trust

| Entity | Trust Level | Justification |
|--------|-------------|---------------|
| Peer Operators | High | Explicit trust establishment |
| libp2p + Noise | High | Well-audited cryptography |
| Docker Engine | Medium | Widely deployed, actively maintained |
| Linux Kernel | Medium | cgroups, namespaces, seccomp |
| Gateway Operator | Low | Only routes traffic, no code execution |

### What We Do NOT Trust

| Entity | Mitigation |
|--------|------------|
| Network | All communication encrypted |
| Untrusted Peers | Connection gating, signature verification |
| Container Code | Strict isolation, resource limits |
| Public Internet | TLS termination, rate limiting |

## Threat Actors

### A1: External Attacker

**Profile**: Remote attacker with no prior access
**Capabilities**: Network access, can send arbitrary traffic
**Goals**: Gain access, exfiltrate data, disrupt service

### A2: Malicious Container

**Profile**: Attacker-controlled code running in a container
**Capabilities**: Full control within container
**Goals**: Container escape, access host resources, lateral movement

### A3: Rogue Peer

**Profile**: Previously trusted peer that turns malicious
**Capabilities**: Can send signed requests
**Goals**: Resource exhaustion, data exfiltration

### A4: Network Attacker

**Profile**: Controls network infrastructure (ISP, router)
**Capabilities**: Traffic interception, modification, replay
**Goals**: Eavesdropping, request forgery

## Attack Vectors and Mitigations

### V1: Unauthorized Connection

**Attack**: Attacker attempts to connect without trust
**Mitigation**: 
- Connection gating at libp2p level
- All connections require mutual authentication
- Untrusted peers are rejected before any protocol messages

```go
// InterceptSecured is called after TLS handshake
func (cg *ConnectionGater) InterceptSecured(dir Direction, p peer.ID, addrs ConnMultiaddrs) bool {
    return cg.trust.IsTrusted(p) // Reject if not trusted
}
```

### V2: Request Forgery

**Attack**: Attacker forges a deployment request
**Mitigation**:
- All requests are signed with Ed25519
- Signature includes timestamp (prevents replay)
- Requester ID verified against signature key

```go
// Request signature verification
payload := CreateSigningPayload(requestID, image, timestamp)
if !peer.PubKey.Verify(payload, signature) {
    return ErrInvalidSignature
}
```

### V3: Replay Attack

**Attack**: Attacker replays a previously captured request
**Mitigation**:
- Timestamp included in signed payload
- Requests rejected if timestamp drift > 5 minutes
- Request IDs tracked to prevent duplicates

### V4: Container Escape

**Attack**: Malicious container attempts to escape isolation
**Mitigation**:
- Non-privileged containers only
- All capabilities dropped
- Seccomp profile restricts syscalls
- No host filesystem mounts
- Network bound to localhost only
- Read-only root filesystem (where possible)

```go
hostConfig := &container.HostConfig{
    Privileged:     false,
    CapDrop:        []string{"ALL"},
    SecurityOpt:    []string{"no-new-privileges:true"},
    NetworkMode:    "bridge",
    Binds:          []string{}, // No host mounts
}
```

### V5: Resource Exhaustion

**Attack**: Attacker deploys containers to exhaust resources
**Mitigation**:
- Strict CPU limits via cgroups
- Memory limits via cgroups
- Maximum container count per peer
- PID limits prevent fork bombs

```go
Resources: container.Resources{
    NanoCPUs:  cpuMillicores * 1000000,
    Memory:    memoryBytes,
    PidsLimit: int64Ptr(100),
}
```

### V6: Man-in-the-Middle

**Attack**: Attacker intercepts P2P communication
**Mitigation**:
- Noise protocol provides forward secrecy
- Mutual authentication via peer IDs
- Public keys verified against peer store

### V7: Gateway Abuse

**Attack**: Attacker abuses gateway for amplification/proxying
**Mitigation**:
- Rate limiting per IP and subdomain
- Timeouts on all connections
- Gateway does not run user code
- Connection limits per tunnel

### V8: Subdomain Takeover

**Attack**: Attacker claims subdomain of stopped deployment
**Mitigation**:
- Subdomains are deterministic (based on deployment ID)
- Subdomain revoked immediately on deployment stop
- Short TTL on DNS records

## Security Invariants

These properties must NEVER be violated:

1. **No container runs privileged**
2. **No container has host filesystem access**
3. **No untrusted peer can send deployment requests**
4. **All P2P traffic is encrypted**
5. **Gateway never executes user code**

## Incident Response

### Rogue Peer Detection

If a trusted peer becomes malicious:
1. Remove from trust list: `peerctl peers remove <peer-id>`
2. Stop all deployments from that peer
3. Rotate identity if compromised

### Container Breach

If container escape is suspected:
1. Stop the daemon immediately
2. Kill all containers: `docker kill $(docker ps -q)`
3. Review container activity logs
4. Report vulnerability

## Security Testing

### Recommended Tests

1. **Fuzzing**: Protocol message parsing
2. **Penetration Testing**: Container escape attempts
3. **Code Review**: Security-critical paths
4. **Dependency Audit**: Check for CVEs

### Security Checklist

- [ ] All connections use encryption
- [ ] Signatures verified on all requests  
- [ ] Resource limits enforced
- [ ] Container isolation verified
- [ ] Rate limiting enabled
- [ ] Logging captures security events

## Known Limitations

1. **DNS hijacking**: Subdomains rely on DNS security
2. **Docker daemon**: Requires trusting the Docker daemon
3. **Kernel bugs**: Container isolation depends on kernel
4. **Timing attacks**: Not fully protected

## References

- [libp2p Security Considerations](https://docs.libp2p.io/concepts/security/)
- [Docker Security Best Practices](https://docs.docker.com/engine/security/)
- [Noise Protocol Framework](https://noiseprotocol.org/)
- [Linux Namespaces and Cgroups](https://man7.org/linux/man-pages/man7/namespaces.7.html)
