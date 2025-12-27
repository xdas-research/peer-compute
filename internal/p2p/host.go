// Package p2p provides peer-to-peer networking capabilities for Peer Compute.
//
// This package uses libp2p to establish secure, encrypted connections between
// peers with mutual authentication based on cryptographic identities.
package p2p

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/multiformats/go-multiaddr"

	"github.com/xdas-research/peer-compute/internal/identity"
)

const (
	// DefaultListenPort is the default port for P2P connections
	DefaultListenPort = 9000
	// ConnectionTimeout is the timeout for establishing connections
	ConnectionTimeout = 30 * time.Second
)

// Host wraps a libp2p host with additional Peer Compute functionality.
type Host struct {
	// host is the underlying libp2p host
	host host.Host
	// identity is the cryptographic identity of this peer
	identity *identity.Identity
	// trust is the trust manager for peer authorization
	trust *TrustManager
	// connGater implements connection gating based on trust
	connGater *ConnectionGater
	// mu protects concurrent access
	mu sync.RWMutex
}

// Config contains configuration options for creating a P2P host.
type Config struct {
	// Identity is the cryptographic identity to use
	Identity *identity.Identity
	// ListenPort is the port to listen on (0 for random)
	ListenPort int
	// ListenAddrs are additional addresses to listen on
	ListenAddrs []string
	// TrustManager is the trust manager for peer authorization
	TrustManager *TrustManager
	// LowWater is the low watermark for connection pruning
	LowWater int
	// HighWater is the high watermark for connection pruning
	HighWater int
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig(id *identity.Identity, tm *TrustManager) *Config {
	return &Config{
		Identity:     id,
		ListenPort:   DefaultListenPort,
		TrustManager: tm,
		LowWater:     100,
		HighWater:    400,
	}
}

// NewHost creates a new P2P host with the given configuration.
//
// SECURITY: The host uses the Noise protocol for encryption, which provides:
// - Forward secrecy
// - Mutual authentication
// - Strong encryption (ChaCha20-Poly1305)
func NewHost(ctx context.Context, cfg *Config) (*Host, error) {
	if cfg.Identity == nil {
		return nil, fmt.Errorf("identity is required")
	}
	if cfg.TrustManager == nil {
		return nil, fmt.Errorf("trust manager is required")
	}

	// Create connection gater for trust-based filtering
	// SECURITY: Connection gater enforces trust at the connection level
	connGater := NewConnectionGater(cfg.TrustManager)

	// Build listen addresses
	port := cfg.ListenPort
	if port == 0 {
		port = DefaultListenPort
	}

	listenAddrs := []string{
		fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port),
		fmt.Sprintf("/ip6/::/tcp/%d", port),
	}
	listenAddrs = append(listenAddrs, cfg.ListenAddrs...)

	// Convert to multiaddrs
	var multiaddrs []multiaddr.Multiaddr
	for _, addr := range listenAddrs {
		ma, err := multiaddr.NewMultiaddr(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid listen address %q: %w", addr, err)
		}
		multiaddrs = append(multiaddrs, ma)
	}

	// Create connection manager for resource limits
	connMgr, err := connmgr.NewConnManager(
		cfg.LowWater,
		cfg.HighWater,
		connmgr.WithGracePeriod(time.Minute),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection manager: %w", err)
	}

	// Create the libp2p host
	// SECURITY: Uses Noise protocol for authenticated encryption
	h, err := libp2p.New(
		// Use our cryptographic identity
		libp2p.Identity(cfg.Identity.PrivKey),
		// Listen on specified addresses
		libp2p.ListenAddrs(multiaddrs...),
		// Use TCP transport
		libp2p.Transport(tcp.NewTCPTransport),
		// Use Noise for security (encrypted + authenticated)
		libp2p.Security(noise.ID, noise.New),
		// Connection gater for trust enforcement
		libp2p.ConnectionGater(connGater),
		// Connection manager for resource limits
		libp2p.ConnectionManager(connMgr),
		// Disable relay (we use direct connections)
		libp2p.DisableRelay(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	return &Host{
		host:      h,
		identity:  cfg.Identity,
		trust:     cfg.TrustManager,
		connGater: connGater,
	}, nil
}

// ID returns this host's peer ID.
func (h *Host) ID() peer.ID {
	return h.host.ID()
}

// Addrs returns the multiaddresses this host is listening on.
func (h *Host) Addrs() []multiaddr.Multiaddr {
	return h.host.Addrs()
}

// AddrInfo returns the full address info for this host (ID + addresses).
func (h *Host) AddrInfo() peer.AddrInfo {
	return peer.AddrInfo{
		ID:    h.host.ID(),
		Addrs: h.host.Addrs(),
	}
}

// Connect attempts to connect to a peer.
// SECURITY: Connection will only succeed if the peer is in the trust list.
func (h *Host) Connect(ctx context.Context, pi peer.AddrInfo) error {
	// Add addresses to peerstore
	h.host.Peerstore().AddAddrs(pi.ID, pi.Addrs, peerstore.PermanentAddrTTL)

	// Connect with timeout
	ctx, cancel := context.WithTimeout(ctx, ConnectionTimeout)
	defer cancel()

	if err := h.host.Connect(ctx, pi); err != nil {
		return fmt.Errorf("failed to connect to peer %s: %w", pi.ID, err)
	}

	return nil
}

// NewStream opens a new stream to a peer with the given protocol.
// SECURITY: Stream will only be opened to trusted peers.
func (h *Host) NewStream(ctx context.Context, peerID peer.ID, proto string) (network.Stream, error) {
	return h.host.NewStream(ctx, peerID, protocol.ID(proto))
}

// SetStreamHandler registers a handler for a protocol.
func (h *Host) SetStreamHandler(proto string, handler network.StreamHandler) {
	h.host.SetStreamHandler(protocol.ID(proto), handler)
}

// Close shuts down the host.
func (h *Host) Close() error {
	return h.host.Close()
}

// Peers returns the list of connected peers.
func (h *Host) Peers() []peer.ID {
	return h.host.Network().Peers()
}

// IsConnected checks if we're connected to a peer.
func (h *Host) IsConnected(peerID peer.ID) bool {
	return h.host.Network().Connectedness(peerID) == network.Connected
}

// Host returns the underlying libp2p host (for advanced use).
func (h *Host) Host() host.Host {
	return h.host
}
