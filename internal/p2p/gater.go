// Package p2p - Connection gating based on trust
package p2p

import (
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// ConnectionGater implements the libp2p ConnectionGater interface to
// enforce trust-based access control at the connection level.
//
// SECURITY CRITICAL: This is the first line of defense against untrusted
// connections. All incoming and outgoing connections are validated against
// the trust list before they are established.
type ConnectionGater struct {
	trust *TrustManager
}

// NewConnectionGater creates a new connection gater.
func NewConnectionGater(trust *TrustManager) *ConnectionGater {
	return &ConnectionGater{trust: trust}
}

// InterceptPeerDial is called before dialing a peer.
// SECURITY: Prevents outgoing connections to untrusted peers.
func (cg *ConnectionGater) InterceptPeerDial(p peer.ID) bool {
	// Allow connections to trusted peers only
	return cg.trust.IsTrusted(p)
}

// InterceptAddrDial is called before dialing an address.
// We don't filter by address, only by peer ID.
func (cg *ConnectionGater) InterceptAddrDial(p peer.ID, addr multiaddr.Multiaddr) bool {
	// Already checked in InterceptPeerDial
	return cg.trust.IsTrusted(p)
}

// InterceptAccept is called when accepting a connection.
// At this point we don't know the peer ID yet, so we allow it.
// The connection will be filtered in InterceptSecured.
func (cg *ConnectionGater) InterceptAccept(addrs network.ConnMultiaddrs) bool {
	// Allow the connection to proceed to the security handshake
	// We'll filter based on peer ID after the handshake
	return true
}

// InterceptSecured is called after the security handshake.
// SECURITY: This is where we filter incoming connections based on peer ID.
func (cg *ConnectionGater) InterceptSecured(dir network.Direction, p peer.ID, addrs network.ConnMultiaddrs) bool {
	// Check if the peer is trusted
	// SECURITY: Reject connections from untrusted peers
	return cg.trust.IsTrusted(p)
}

// InterceptUpgraded is called after the connection is upgraded.
// We always allow at this point since we've already verified trust.
func (cg *ConnectionGater) InterceptUpgraded(conn network.Conn) (bool, control.DisconnectReason) {
	// Connection has already passed all checks
	return true, 0
}
