// Package client provides P2P client functionality for sending requests to providers.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xdas-research/peer-compute/internal/p2p"
	"github.com/xdas-research/peer-compute/internal/protocol"
)

// Client is a P2P client for sending requests to providers.
type Client struct {
	host *p2p.Host
}

// NewClient creates a new P2P client.
func NewClient(host *p2p.Host) *Client {
	return &Client{host: host}
}

// Deploy sends a deployment request to a provider.
func (c *Client) Deploy(ctx context.Context, peerID peer.ID, req *protocol.DeployRequest) (*protocol.DeployResponse, error) {
	// Open stream to peer
	stream, err := c.host.NewStream(ctx, peerID, "/peercompute/deploy/1.0.0")
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	// Send request
	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Read response
	var resp protocol.DeployResponse
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return &resp, nil
}

// Stop sends a stop request to a provider.
func (c *Client) Stop(ctx context.Context, peerID peer.ID, deploymentID string) (*protocol.StopResponse, error) {
	stream, err := c.host.NewStream(ctx, peerID, "/peercompute/stop/1.0.0")
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	req := protocol.StopRequest{
		DeploymentID: deploymentID,
	}

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	var resp protocol.StopResponse
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return &resp, nil
}

// Status gets deployment status from a provider.
func (c *Client) Status(ctx context.Context, peerID peer.ID, deploymentID string) (*protocol.StatusResponse, error) {
	stream, err := c.host.NewStream(ctx, peerID, "/peercompute/status/1.0.0")
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	req := protocol.StatusRequest{
		DeploymentID: deploymentID,
	}

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	var resp protocol.StatusResponse
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return &resp, nil
}

// Logs streams logs from a deployment.
func (c *Client) Logs(ctx context.Context, peerID peer.ID, deploymentID string, follow bool) (io.ReadCloser, error) {
	stream, err := c.host.NewStream(ctx, peerID, "/peercompute/logs/1.0.0")
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}

	req := struct {
		DeploymentID string `json:"deployment_id"`
		Follow       bool   `json:"follow"`
	}{
		DeploymentID: deploymentID,
		Follow:       follow,
	}

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(req); err != nil {
		stream.Close()
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Return stream for reading logs
	return stream, nil
}
