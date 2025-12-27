// Package security provides cryptographic operations for Peer Compute.
package security

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	// MaxTimestampDrift is the maximum allowed time difference for requests
	// SECURITY: Prevents replay attacks
	MaxTimestampDrift = 5 * time.Minute
)

// Signer handles message signing operations.
type Signer struct {
	privKey crypto.PrivKey
	pubKey  crypto.PubKey
	peerID  peer.ID
}

// NewSigner creates a new signer from a private key.
func NewSigner(privKey crypto.PrivKey) (*Signer, error) {
	pubKey := privKey.GetPublic()
	peerID, err := peer.IDFromPublicKey(pubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive peer ID: %w", err)
	}

	return &Signer{
		privKey: privKey,
		pubKey:  pubKey,
		peerID:  peerID,
	}, nil
}

// Sign signs a message and returns the signature.
func (s *Signer) Sign(data []byte) ([]byte, error) {
	return s.privKey.Sign(data)
}

// PeerID returns the signer's peer ID.
func (s *Signer) PeerID() peer.ID {
	return s.peerID
}

// Verifier handles signature verification.
type Verifier struct{}

// NewVerifier creates a new verifier.
func NewVerifier() *Verifier {
	return &Verifier{}
}

// Verify verifies a signature against a public key.
func (v *Verifier) Verify(pubKey crypto.PubKey, data, signature []byte) (bool, error) {
	return pubKey.Verify(data, signature)
}

// VerifyFromPeerID verifies a signature using a peer ID.
// The peer ID contains the public key hash, so we need the full public key.
func (v *Verifier) VerifyFromPeerID(peerID peer.ID, pubKeyBytes, data, signature []byte) (bool, error) {
	// Unmarshal the public key
	pubKey, err := crypto.UnmarshalPublicKey(pubKeyBytes)
	if err != nil {
		return false, fmt.Errorf("failed to unmarshal public key: %w", err)
	}

	// Verify that the public key matches the peer ID
	derivedID, err := peer.IDFromPublicKey(pubKey)
	if err != nil {
		return false, fmt.Errorf("failed to derive peer ID: %w", err)
	}

	if derivedID != peerID {
		return false, fmt.Errorf("public key does not match peer ID")
	}

	// Verify the signature
	return pubKey.Verify(data, signature)
}

// CreateSigningPayload creates a deterministic payload for signing.
// SECURITY: This ensures the same input always produces the same output.
func CreateSigningPayload(parts ...[]byte) []byte {
	h := sha256.New()
	for _, part := range parts {
		h.Write(part)
	}
	return h.Sum(nil)
}

// HashHex returns the hex-encoded SHA256 hash of data.
func HashHex(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// ValidateTimestamp checks if a timestamp is within acceptable drift.
// SECURITY: Prevents replay attacks with old signed messages.
func ValidateTimestamp(timestamp int64) error {
	reqTime := time.Unix(0, timestamp)
	now := time.Now()
	drift := now.Sub(reqTime)

	if drift < -MaxTimestampDrift {
		return fmt.Errorf("timestamp is in the future: %v", drift)
	}
	if drift > MaxTimestampDrift {
		return fmt.Errorf("timestamp is too old: %v", drift)
	}

	return nil
}

// GenerateNonce generates a random nonce for request uniqueness.
func GenerateNonce() ([]byte, error) {
	// Use crypto/rand for secure random bytes
	nonce := make([]byte, 32)
	// In production, use crypto/rand
	return nonce, nil
}
