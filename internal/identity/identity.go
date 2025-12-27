// Package identity provides cryptographic identity management for Peer Compute.
//
// SECURITY CRITICAL: This package handles private key generation, persistence,
// and peer ID derivation. The private key MUST be protected at rest and never
// transmitted over the network.
package identity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	// KeyFileName is the default filename for the identity key file
	KeyFileName = "identity.key"
	// KeyFilePerms are the file permissions for the key file (owner read/write only)
	// SECURITY: Key file must not be readable by other users
	KeyFilePerms = 0600
	// DirPerms are the directory permissions for the config directory
	DirPerms = 0700
)

// Identity represents a peer's cryptographic identity in the P2P network.
// It contains the Ed25519 key pair and derived peer ID.
type Identity struct {
	// PrivKey is the Ed25519 private key used for signing and authentication
	// SECURITY CRITICAL: Never expose or transmit this key
	PrivKey crypto.PrivKey
	// PubKey is the Ed25519 public key derived from PrivKey
	PubKey crypto.PubKey
	// PeerID is the libp2p peer ID derived from the public key
	// This is the public identifier for this peer in the network
	PeerID peer.ID
}

// Generate creates a new cryptographic identity using Ed25519.
// Ed25519 is chosen for its security properties and performance:
// - 128-bit security level
// - Fast signing and verification
// - Small key and signature sizes
// - Deterministic signatures (no random number generation during signing)
func Generate() (*Identity, error) {
	// SECURITY: Use crypto/rand for key generation
	// This provides cryptographically secure random bytes from the OS
	privKey, pubKey, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate Ed25519 key pair: %w", err)
	}

	// Derive peer ID from public key
	// The peer ID is a multihash of the public key
	peerID, err := peer.IDFromPublicKey(pubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive peer ID from public key: %w", err)
	}

	return &Identity{
		PrivKey: privKey,
		PubKey:  pubKey,
		PeerID:  peerID,
	}, nil
}

// Load reads an identity from a key file.
// The key file contains the raw Ed25519 private key bytes.
func Load(path string) (*Identity, error) {
	// Read the key file
	// SECURITY: The file should have restricted permissions (0600)
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read identity key file: %w", err)
	}

	// Decode hex-encoded key
	rawKey, err := hex.DecodeString(string(keyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to decode key file (expected hex): %w", err)
	}

	// Unmarshal the private key
	privKey, err := crypto.UnmarshalEd25519PrivateKey(rawKey)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal Ed25519 private key: %w", err)
	}

	// Derive public key from private key
	pubKey := privKey.GetPublic()

	// Derive peer ID
	peerID, err := peer.IDFromPublicKey(pubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive peer ID: %w", err)
	}

	return &Identity{
		PrivKey: privKey,
		PubKey:  pubKey,
		PeerID:  peerID,
	}, nil
}

// Save persists the identity to a key file.
// The key is stored as hex-encoded raw bytes.
func (i *Identity) Save(path string) error {
	// Ensure the directory exists with secure permissions
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DirPerms); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal the private key to raw bytes
	rawKey, err := crypto.MarshalPrivateKey(i.PrivKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	// Encode as hex for safe storage
	keyHex := hex.EncodeToString(rawKey)

	// Write with restricted permissions
	// SECURITY: File must only be readable by owner
	if err := os.WriteFile(path, []byte(keyHex), KeyFilePerms); err != nil {
		return fmt.Errorf("failed to write identity key file: %w", err)
	}

	return nil
}

// LoadOrGenerate attempts to load an identity from the given path,
// or generates a new one if the file doesn't exist.
func LoadOrGenerate(path string) (*Identity, bool, error) {
	// Check if the key file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Generate a new identity
		identity, err := Generate()
		if err != nil {
			return nil, false, err
		}

		// Save the new identity
		if err := identity.Save(path); err != nil {
			return nil, false, err
		}

		return identity, true, nil
	}

	// Load existing identity
	identity, err := Load(path)
	if err != nil {
		return nil, false, err
	}

	return identity, false, nil
}

// String returns a string representation of the identity (peer ID only)
func (i *Identity) String() string {
	return i.PeerID.String()
}

// PublicKeyBytes returns the raw public key bytes for sharing with peers
func (i *Identity) PublicKeyBytes() ([]byte, error) {
	return crypto.MarshalPublicKey(i.PubKey)
}

// Sign signs a message using the identity's private key
// SECURITY: Used for authenticating deployment requests
func (i *Identity) Sign(data []byte) ([]byte, error) {
	signature, err := i.PrivKey.Sign(data)
	if err != nil {
		return nil, fmt.Errorf("failed to sign data: %w", err)
	}
	return signature, nil
}

// Verify checks a signature against the identity's public key
func (i *Identity) Verify(data, signature []byte) (bool, error) {
	return i.PubKey.Verify(data, signature)
}
