// Package p2p - Trust management for peer authorization
package p2p

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// TrustedPeer represents a peer that has been explicitly trusted.
type TrustedPeer struct {
	// ID is the peer's libp2p peer ID
	ID peer.ID `json:"id"`
	// Name is an optional human-readable name for the peer
	Name string `json:"name,omitempty"`
	// AddedAt is when the peer was added to the trust list
	AddedAt time.Time `json:"added_at"`
	// Addresses are known multiaddresses for this peer
	Addresses []string `json:"addresses,omitempty"`
}

// TrustManager manages the list of trusted peers.
//
// SECURITY: This implements explicit trust - peers must be manually added
// to the trust list before any communication is allowed. This is a core
// security property of Peer Compute.
type TrustManager struct {
	// peers maps peer ID to trusted peer info
	peers map[peer.ID]*TrustedPeer
	// path is the file path for persisting trusted peers
	path string
	// mu protects concurrent access
	mu sync.RWMutex
}

// NewTrustManager creates a new trust manager.
func NewTrustManager(path string) *TrustManager {
	return &TrustManager{
		peers: make(map[peer.ID]*TrustedPeer),
		path:  path,
	}
}

// Load reads the trusted peers from the persistence file.
func (tm *TrustManager) Load() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Check if file exists
	if _, err := os.Stat(tm.path); os.IsNotExist(err) {
		// No trusted peers file yet, start with empty list
		return nil
	}

	data, err := os.ReadFile(tm.path)
	if err != nil {
		return fmt.Errorf("failed to read trusted peers file: %w", err)
	}

	var peers []*TrustedPeer
	if err := json.Unmarshal(data, &peers); err != nil {
		return fmt.Errorf("failed to parse trusted peers file: %w", err)
	}

	tm.peers = make(map[peer.ID]*TrustedPeer)
	for _, p := range peers {
		tm.peers[p.ID] = p
	}

	return nil
}

// Save persists the trusted peers to the file.
func (tm *TrustManager) Save() error {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// Convert map to slice for JSON
	peers := make([]*TrustedPeer, 0, len(tm.peers))
	for _, p := range tm.peers {
		peers = append(peers, p)
	}

	data, err := json.MarshalIndent(peers, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal trusted peers: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(tm.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Write with restricted permissions
	if err := os.WriteFile(tm.path, data, 0600); err != nil {
		return fmt.Errorf("failed to write trusted peers file: %w", err)
	}

	return nil
}

// Add adds a peer to the trust list.
// SECURITY: This is the only way to establish trust with a peer.
func (tm *TrustManager) Add(peerID peer.ID, name string, addresses []string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, exists := tm.peers[peerID]; exists {
		// Update existing peer
		tm.peers[peerID].Name = name
		tm.peers[peerID].Addresses = addresses
	} else {
		// Add new peer
		tm.peers[peerID] = &TrustedPeer{
			ID:        peerID,
			Name:      name,
			AddedAt:   time.Now(),
			Addresses: addresses,
		}
	}

	return tm.saveUnlocked()
}

// Remove removes a peer from the trust list.
// SECURITY: After removal, all connections from this peer will be rejected.
func (tm *TrustManager) Remove(peerID peer.ID) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, exists := tm.peers[peerID]; !exists {
		return fmt.Errorf("peer %s not in trust list", peerID)
	}

	delete(tm.peers, peerID)
	return tm.saveUnlocked()
}

// IsTrusted checks if a peer is in the trust list.
// SECURITY: This is called by the connection gater to authorize connections.
func (tm *TrustManager) IsTrusted(peerID peer.ID) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	_, exists := tm.peers[peerID]
	return exists
}

// Get returns information about a trusted peer.
func (tm *TrustManager) Get(peerID peer.ID) (*TrustedPeer, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	p, exists := tm.peers[peerID]
	if !exists {
		return nil, false
	}
	// Return a copy to prevent mutation
	copy := *p
	return &copy, true
}

// List returns all trusted peers.
func (tm *TrustManager) List() []*TrustedPeer {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	peers := make([]*TrustedPeer, 0, len(tm.peers))
	for _, p := range tm.peers {
		copy := *p
		peers = append(peers, &copy)
	}
	return peers
}

// Count returns the number of trusted peers.
func (tm *TrustManager) Count() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.peers)
}

// saveUnlocked saves without acquiring the lock (caller must hold lock)
func (tm *TrustManager) saveUnlocked() error {
	peers := make([]*TrustedPeer, 0, len(tm.peers))
	for _, p := range tm.peers {
		peers = append(peers, p)
	}

	data, err := json.MarshalIndent(peers, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal trusted peers: %w", err)
	}

	dir := filepath.Dir(tm.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(tm.path, data, 0600); err != nil {
		return fmt.Errorf("failed to write trusted peers file: %w", err)
	}

	return nil
}
