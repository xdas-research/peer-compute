// Package p2p - Peer discovery mechanisms
package p2p

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/multiformats/go-multiaddr"
)

const (
	// MDNSServiceName is the mDNS service tag for peer discovery
	MDNSServiceName = "_peercompute._tcp"
	// DiscoveryInterval is how often to advertise via mDNS
	DiscoveryInterval = 10 * time.Second
)

// Discovery handles peer discovery via mDNS.
// mDNS is used for local network discovery without requiring any infrastructure.
type Discovery struct {
	host   host.Host
	trust  *TrustManager
	mdns   mdns.Service
	notify chan peer.AddrInfo
	mu     sync.RWMutex
}

// discoveryNotifee implements the mdns.Notifee interface
type discoveryNotifee struct {
	discovery *Discovery
}

func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	n.discovery.handlePeerFound(pi)
}

// NewDiscovery creates a new discovery service.
func NewDiscovery(h host.Host, trust *TrustManager) *Discovery {
	return &Discovery{
		host:   h,
		trust:  trust,
		notify: make(chan peer.AddrInfo, 10),
	}
}

// Start begins peer discovery via mDNS.
func (d *Discovery) Start(ctx context.Context) error {
	// Create mDNS service
	notifee := &discoveryNotifee{discovery: d}
	service := mdns.NewMdnsService(d.host, MDNSServiceName, notifee)
	if err := service.Start(); err != nil {
		return fmt.Errorf("failed to start mDNS service: %w", err)
	}

	d.mu.Lock()
	d.mdns = service
	d.mu.Unlock()

	return nil
}

// Stop stops the discovery service.
func (d *Discovery) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.mdns != nil {
		return d.mdns.Close()
	}
	return nil
}

// handlePeerFound is called when a peer is discovered via mDNS.
func (d *Discovery) handlePeerFound(pi peer.AddrInfo) {
	// Don't notify about ourselves
	if pi.ID == d.host.ID() {
		return
	}

	// Only notify about trusted peers
	// SECURITY: We only connect to peers in our trust list
	if !d.trust.IsTrusted(pi.ID) {
		return
	}

	// Send to notification channel (non-blocking)
	select {
	case d.notify <- pi:
	default:
		// Channel full, skip notification
	}
}

// PeerFound returns a channel that receives discovered trusted peers.
func (d *Discovery) PeerFound() <-chan peer.AddrInfo {
	return d.notify
}

// ConnectToPeers attempts to connect to all known trusted peers.
func (d *Discovery) ConnectToPeers(ctx context.Context, h *Host) []error {
	var errors []error

	for _, tp := range d.trust.List() {
		if len(tp.Addresses) == 0 {
			continue
		}

		// Parse addresses
		pi, err := ParseAddrInfo(tp.ID.String(), tp.Addresses)
		if err != nil {
			errors = append(errors, fmt.Errorf("invalid address for peer %s: %w", tp.ID, err))
			continue
		}

		// Try to connect
		if err := h.Connect(ctx, pi); err != nil {
			errors = append(errors, fmt.Errorf("failed to connect to peer %s: %w", tp.ID, err))
			continue
		}
	}

	return errors
}

// ParseAddrInfo parses a peer ID and addresses into an AddrInfo.
func ParseAddrInfo(peerIDStr string, addrs []string) (peer.AddrInfo, error) {
	peerID, err := peer.Decode(peerIDStr)
	if err != nil {
		return peer.AddrInfo{}, fmt.Errorf("invalid peer ID: %w", err)
	}

	pi := peer.AddrInfo{ID: peerID}
	for _, addr := range addrs {
		ma, err := multiaddr.NewMultiaddr(addr)
		if err != nil {
			return peer.AddrInfo{}, fmt.Errorf("invalid address %q: %w", addr, err)
		}
		pi.Addrs = append(pi.Addrs, ma)
	}

	return pi, nil
}
