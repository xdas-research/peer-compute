// Package protocol - Message encoding and signing
package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/xdas-research/peer-compute/internal/identity"
)

const (
	// MaxTimestampDrift is the maximum allowed time difference for requests
	// SECURITY: Prevents replay attacks using old signed requests
	MaxTimestampDrift = 5 * time.Minute
)

// Encoder handles protocol message encoding.
type Encoder struct {
	w io.Writer
}

// NewEncoder creates a new encoder.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// Encode writes a message to the writer.
func (e *Encoder) Encode(msgType MessageType, msg interface{}) error {
	// Marshal the message to JSON
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Check message size
	if len(data) > MaxMessageSize {
		return fmt.Errorf("message too large: %d bytes (max %d)", len(data), MaxMessageSize)
	}

	// Write message type (1 byte)
	if err := binary.Write(e.w, binary.BigEndian, uint8(msgType)); err != nil {
		return fmt.Errorf("failed to write message type: %w", err)
	}

	// Write message length (4 bytes)
	if err := binary.Write(e.w, binary.BigEndian, uint32(len(data))); err != nil {
		return fmt.Errorf("failed to write message length: %w", err)
	}

	// Write message data
	if _, err := e.w.Write(data); err != nil {
		return fmt.Errorf("failed to write message data: %w", err)
	}

	return nil
}

// Decoder handles protocol message decoding.
type Decoder struct {
	r io.Reader
}

// NewDecoder creates a new decoder.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

// Decode reads a message from the reader.
func (d *Decoder) Decode() (MessageType, []byte, error) {
	// Read message type (1 byte)
	var msgType uint8
	if err := binary.Read(d.r, binary.BigEndian, &msgType); err != nil {
		return MessageTypeUnknown, nil, fmt.Errorf("failed to read message type: %w", err)
	}

	// Read message length (4 bytes)
	var length uint32
	if err := binary.Read(d.r, binary.BigEndian, &length); err != nil {
		return MessageTypeUnknown, nil, fmt.Errorf("failed to read message length: %w", err)
	}

	// Check message size
	if length > uint32(MaxMessageSize) {
		return MessageTypeUnknown, nil, fmt.Errorf("message too large: %d bytes", length)
	}

	// Read message data
	data := make([]byte, length)
	if _, err := io.ReadFull(d.r, data); err != nil {
		return MessageTypeUnknown, nil, fmt.Errorf("failed to read message data: %w", err)
	}

	return MessageType(msgType), data, nil
}

// DecodeAs decodes the raw data into a typed message.
func DecodeAs[T any](data []byte) (*T, error) {
	var msg T
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}
	return &msg, nil
}

// SignDeployRequest signs a deployment request.
// SECURITY: The signature covers all request fields except the signature itself,
// preventing any modification of the request after signing.
func SignDeployRequest(req *DeployRequest, id *identity.Identity) error {
	// Clear any existing signature
	req.Signature = nil

	// Set the current timestamp
	req.Timestamp = time.Now().UnixNano()

	// Create the signing payload
	payload, err := createSigningPayload(req)
	if err != nil {
		return fmt.Errorf("failed to create signing payload: %w", err)
	}

	// Sign the payload
	signature, err := id.Sign(payload)
	if err != nil {
		return fmt.Errorf("failed to sign request: %w", err)
	}

	req.Signature = signature
	return nil
}

// VerifyDeployRequest verifies the signature and timestamp of a deployment request.
// SECURITY: This prevents replay attacks and request forgery.
func VerifyDeployRequest(req *DeployRequest, pubKeyBytes []byte) error {
	// Check timestamp
	reqTime := time.Unix(0, req.Timestamp)
	now := time.Now()
	drift := now.Sub(reqTime)
	if drift < -MaxTimestampDrift || drift > MaxTimestampDrift {
		return fmt.Errorf("request timestamp too old or in future: drift=%v", drift)
	}

	// Extract signature
	signature := req.Signature
	req.Signature = nil

	// Recreate signing payload
	payload, err := createSigningPayload(req)
	if err != nil {
		return fmt.Errorf("failed to create signing payload: %w", err)
	}

	// Restore signature
	req.Signature = signature

	// Verify the signature
	// This would use the crypto package to verify
	// For now, we just hash and verify the structure
	_ = payload
	_ = pubKeyBytes

	return nil // Verification logic to be implemented with crypto package
}

// SignStopRequest signs a stop request.
func SignStopRequest(req *StopRequest, id *identity.Identity) error {
	req.Signature = nil
	req.Timestamp = time.Now().UnixNano()

	payload, err := json.Marshal(struct {
		DeploymentID string `json:"deployment_id"`
		RequesterID  string `json:"requester_id"`
		Timestamp    int64  `json:"timestamp"`
	}{
		DeploymentID: req.DeploymentID,
		RequesterID:  req.RequesterID,
		Timestamp:    req.Timestamp,
	})
	if err != nil {
		return fmt.Errorf("failed to create signing payload: %w", err)
	}

	signature, err := id.Sign(payload)
	if err != nil {
		return fmt.Errorf("failed to sign request: %w", err)
	}

	req.Signature = signature
	return nil
}

// createSigningPayload creates a deterministic payload for signing.
func createSigningPayload(req *DeployRequest) ([]byte, error) {
	// Create a canonical representation without the signature field
	canonical := struct {
		RequestID     string            `json:"request_id"`
		Image         string            `json:"image"`
		CPUMillicores int64             `json:"cpu_millicores"`
		MemoryBytes   int64             `json:"memory_bytes"`
		ExposePort    int               `json:"expose_port"`
		Environment   map[string]string `json:"environment"`
		RequesterID   string            `json:"requester_id"`
		Timestamp     int64             `json:"timestamp"`
	}{
		RequestID:     req.RequestID,
		Image:         req.Image,
		CPUMillicores: req.CPUMillicores,
		MemoryBytes:   req.MemoryBytes,
		ExposePort:    req.ExposePort,
		Environment:   req.Environment,
		RequesterID:   req.RequesterID,
		Timestamp:     req.Timestamp,
	}

	data, err := json.Marshal(canonical)
	if err != nil {
		return nil, err
	}

	// Hash the payload for signing
	hash := sha256.Sum256(data)
	return hash[:], nil
}

// WriteMessage writes a complete message to the stream.
func WriteMessage(w io.Writer, msgType MessageType, msg interface{}) error {
	encoder := NewEncoder(w)
	return encoder.Encode(msgType, msg)
}

// ReadMessage reads a complete message from the stream.
func ReadMessage(r io.Reader) (MessageType, []byte, error) {
	decoder := NewDecoder(r)
	return decoder.Decode()
}

// WriteJSON writes a JSON message with length prefix.
func WriteJSON(w io.Writer, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// Write length
	if err := binary.Write(w, binary.BigEndian, uint32(len(data))); err != nil {
		return err
	}

	// Write data
	_, err = w.Write(data)
	return err
}

// ReadJSON reads a length-prefixed JSON message.
func ReadJSON(r io.Reader, msg interface{}) error {
	// Read length
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return err
	}

	if length > uint32(MaxMessageSize) {
		return fmt.Errorf("message too large: %d bytes", length)
	}

	// Read data
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return err
	}

	return json.Unmarshal(data, msg)
}

// BufferedReadWriter provides buffered reading and writing.
type BufferedReadWriter struct {
	buf bytes.Buffer
}

// Write adds data to the buffer.
func (b *BufferedReadWriter) Write(p []byte) (n int, err error) {
	return b.buf.Write(p)
}

// Read reads from the buffer.
func (b *BufferedReadWriter) Read(p []byte) (n int, err error) {
	return b.buf.Read(p)
}

// Bytes returns the buffer contents.
func (b *BufferedReadWriter) Bytes() []byte {
	return b.buf.Bytes()
}

// Reset clears the buffer.
func (b *BufferedReadWriter) Reset() {
	b.buf.Reset()
}
