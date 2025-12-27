// Package protocol defines the message types and handlers for Peer Compute P2P communication.
//
// All protocol messages are encoded using Protocol Buffers for efficiency and
// cross-language compatibility. Messages are signed by the sender to ensure
// authenticity and prevent tampering.
package protocol

import (
	"time"
)

const (
	// ProtocolID is the libp2p protocol identifier for Peer Compute
	ProtocolID = "/peercompute/1.0.0"

	// DeployProtocol is the protocol for deployment requests
	DeployProtocol = "/peercompute/deploy/1.0.0"

	// LogProtocol is the protocol for log streaming
	LogProtocol = "/peercompute/logs/1.0.0"

	// StatusProtocol is the protocol for status updates
	StatusProtocol = "/peercompute/status/1.0.0"

	// MaxMessageSize is the maximum size of a protocol message (10MB)
	MaxMessageSize = 10 * 1024 * 1024

	// RequestTimeout is the default timeout for requests
	RequestTimeout = 30 * time.Second
)

// MessageType identifies the type of protocol message
type MessageType uint8

const (
	MessageTypeUnknown MessageType = iota
	MessageTypeDeployRequest
	MessageTypeDeployResponse
	MessageTypeStopRequest
	MessageTypeStopResponse
	MessageTypeLogEntry
	MessageTypeStatusRequest
	MessageTypeStatusResponse
)

// DeployRequest represents a request to deploy a container.
type DeployRequest struct {
	// RequestID is a unique identifier for this request
	RequestID string `json:"request_id"`

	// Image is the Docker image to deploy (e.g., "nginx:alpine")
	Image string `json:"image"`

	// CPUMillicores is the CPU limit in millicores (1000 = 1 CPU)
	CPUMillicores int64 `json:"cpu_millicores"`

	// MemoryBytes is the memory limit in bytes
	MemoryBytes int64 `json:"memory_bytes"`

	// ExposePort is the container port to expose (0 = no exposure)
	ExposePort int `json:"expose_port,omitempty"`

	// Environment is a map of environment variables
	Environment map[string]string `json:"environment,omitempty"`

	// RequesterID is the peer ID of the requester
	RequesterID string `json:"requester_id"`

	// Timestamp is when the request was created (for replay protection)
	Timestamp int64 `json:"timestamp"`

	// Signature is the Ed25519 signature of the request
	Signature []byte `json:"signature"`
}

// DeployResponse is the response to a deployment request.
type DeployResponse struct {
	// RequestID matches the request
	RequestID string `json:"request_id"`

	// DeploymentID is the unique identifier for this deployment
	DeploymentID string `json:"deployment_id,omitempty"`

	// Status is the deployment status
	Status DeploymentStatus `json:"status"`

	// ExposedURL is the public URL if the container was exposed
	ExposedURL string `json:"exposed_url,omitempty"`

	// ContainerID is the Docker container ID
	ContainerID string `json:"container_id,omitempty"`

	// Error is the error message if the deployment failed
	Error string `json:"error,omitempty"`
}

// DeploymentStatus represents the status of a deployment.
type DeploymentStatus string

const (
	StatusPending     DeploymentStatus = "pending"
	StatusPulling     DeploymentStatus = "pulling"
	StatusStarting    DeploymentStatus = "starting"
	StatusRunning     DeploymentStatus = "running"
	StatusStopping    DeploymentStatus = "stopping"
	StatusStopped     DeploymentStatus = "stopped"
	StatusFailed      DeploymentStatus = "failed"
	StatusTerminated  DeploymentStatus = "terminated"
)

// StopRequest is a request to stop a deployment.
type StopRequest struct {
	// DeploymentID is the deployment to stop
	DeploymentID string `json:"deployment_id"`

	// RequesterID is the peer ID of the requester
	RequesterID string `json:"requester_id"`

	// Timestamp is when the request was created
	Timestamp int64 `json:"timestamp"`

	// Signature is the Ed25519 signature
	Signature []byte `json:"signature"`
}

// StopResponse is the response to a stop request.
type StopResponse struct {
	// DeploymentID is the deployment that was stopped
	DeploymentID string `json:"deployment_id"`

	// Success indicates if the stop was successful
	Success bool `json:"success"`

	// Error is the error message if the stop failed
	Error string `json:"error,omitempty"`
}

// LogEntry represents a log message from a container.
type LogEntry struct {
	// DeploymentID identifies the deployment
	DeploymentID string `json:"deployment_id"`

	// Timestamp is when the log was generated
	Timestamp int64 `json:"timestamp"`

	// Stream is "stdout" or "stderr"
	Stream string `json:"stream"`

	// Data is the log content
	Data []byte `json:"data"`
}

// StatusRequest is a request for deployment status.
type StatusRequest struct {
	// DeploymentID is the deployment to query
	DeploymentID string `json:"deployment_id"`
}

// StatusResponse is the response to a status request.
type StatusResponse struct {
	// DeploymentID is the deployment ID
	DeploymentID string `json:"deployment_id"`

	// Status is the current status
	Status DeploymentStatus `json:"status"`

	// Image is the container image
	Image string `json:"image"`

	// StartedAt is when the container started
	StartedAt int64 `json:"started_at,omitempty"`

	// ExposedURL is the public URL if exposed
	ExposedURL string `json:"exposed_url,omitempty"`

	// ResourceUsage contains current resource usage
	ResourceUsage *ResourceUsage `json:"resource_usage,omitempty"`

	// Error is set if the deployment failed
	Error string `json:"error,omitempty"`
}

// ResourceUsage contains resource usage metrics.
type ResourceUsage struct {
	// CPUPercent is the CPU usage percentage
	CPUPercent float64 `json:"cpu_percent"`

	// MemoryBytes is the current memory usage
	MemoryBytes int64 `json:"memory_bytes"`

	// MemoryLimit is the memory limit
	MemoryLimit int64 `json:"memory_limit"`
}

// Deployment represents an active deployment on a provider.
type Deployment struct {
	// ID is the unique deployment identifier
	ID string `json:"id"`

	// Image is the Docker image
	Image string `json:"image"`

	// ContainerID is the Docker container ID
	ContainerID string `json:"container_id"`

	// RequesterID is who requested this deployment
	RequesterID string `json:"requester_id"`

	// Status is the current status
	Status DeploymentStatus `json:"status"`

	// CPULimit is the CPU limit in millicores
	CPULimit int64 `json:"cpu_limit"`

	// MemoryLimit is the memory limit in bytes
	MemoryLimit int64 `json:"memory_limit"`

	// ExposePort is the exposed container port
	ExposePort int `json:"expose_port,omitempty"`

	// ExposedURL is the public URL
	ExposedURL string `json:"exposed_url,omitempty"`

	// TunnelID is the reverse tunnel ID
	TunnelID string `json:"tunnel_id,omitempty"`

	// StartedAt is when the deployment started
	StartedAt time.Time `json:"started_at"`

	// StoppedAt is when the deployment stopped
	StoppedAt *time.Time `json:"stopped_at,omitempty"`
}
