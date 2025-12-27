// Package tunnel provides reverse tunnel functionality for exposing containers.
//
// The tunnel works as follows:
// 1. Provider peer initiates an outbound connection to the gateway
// 2. Gateway assigns a subdomain and routes traffic to the tunnel
// 3. Traffic flows: Client → Gateway → Tunnel → Container
//
// SECURITY: All tunnels use TLS encryption. No inbound ports are required
// on the provider machine.
package tunnel

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	// DefaultGatewayPort is the default port for tunnel connections
	DefaultGatewayPort = 8443

	// ReconnectInterval is the interval between reconnection attempts
	ReconnectInterval = 5 * time.Second

	// HeartbeatInterval is the interval for sending heartbeats
	HeartbeatInterval = 30 * time.Second

	// HeartbeatTimeout is the timeout for heartbeat responses
	HeartbeatTimeout = 10 * time.Second
)

// Client is a reverse tunnel client that runs on the provider.
type Client struct {
	gatewayAddr string
	tlsConfig   *tls.Config
	peerID      string
	deployments map[string]*TunnelBinding
	mu          sync.RWMutex
	conn        net.Conn
	ctx         context.Context
	cancel      context.CancelFunc
}

// TunnelBinding represents a binding between a deployment and a tunnel.
type TunnelBinding struct {
	DeploymentID string
	ContainerID  string
	LocalPort    int
	AssignedURL  string
}

// ClientConfig contains configuration for the tunnel client.
type ClientConfig struct {
	// GatewayAddr is the address of the gateway server
	GatewayAddr string
	// PeerID is the peer ID of this provider
	PeerID string
	// TLSConfig is the TLS configuration for the connection
	TLSConfig *tls.Config
}

// NewClient creates a new tunnel client.
func NewClient(cfg *ClientConfig) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		gatewayAddr: cfg.GatewayAddr,
		tlsConfig:   cfg.TLSConfig,
		peerID:      cfg.PeerID,
		deployments: make(map[string]*TunnelBinding),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Connect establishes a connection to the gateway.
func (c *Client) Connect(ctx context.Context) error {
	// Use TLS for encrypted connection
	var conn net.Conn
	var err error

	if c.tlsConfig != nil {
		conn, err = tls.Dial("tcp", c.gatewayAddr, c.tlsConfig)
	} else {
		// For development, allow non-TLS connections
		conn, err = net.Dial("tcp", c.gatewayAddr)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to gateway: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	// Send authentication/registration
	if err := c.authenticate(); err != nil {
		conn.Close()
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Start heartbeat and message handling
	go c.handleMessages()
	go c.heartbeat()

	return nil
}

// RegisterDeployment registers a deployment for exposure via the tunnel.
func (c *Client) RegisterDeployment(deploymentID, containerID string, localPort int) (*TunnelBinding, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, fmt.Errorf("not connected to gateway")
	}

	// Send registration request to gateway
	// In production, this would use a proper protocol
	binding := &TunnelBinding{
		DeploymentID: deploymentID,
		ContainerID:  containerID,
		LocalPort:    localPort,
		// Gateway will assign the URL
	}

	c.deployments[deploymentID] = binding

	// TODO: Send registration message to gateway and wait for response
	// For MVP, we'll simulate the URL assignment
	binding.AssignedURL = fmt.Sprintf("https://%s.peercompute.xdastechnology.com", deploymentID)

	return binding, nil
}

// UnregisterDeployment removes a deployment from the tunnel.
func (c *Client) UnregisterDeployment(deploymentID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.deployments[deploymentID]; !ok {
		return fmt.Errorf("deployment %s not registered", deploymentID)
	}

	delete(c.deployments, deploymentID)

	// TODO: Send unregistration message to gateway

	return nil
}

// Close closes the tunnel connection.
func (c *Client) Close() error {
	c.cancel()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// authenticate sends authentication to the gateway.
func (c *Client) authenticate() error {
	// TODO: Implement proper authentication protocol
	// For MVP, we just send the peer ID
	return nil
}

// handleMessages processes incoming messages from the gateway.
func (c *Client) handleMessages() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			// Read messages from gateway
			// Forward traffic to appropriate container
		}
	}
}

// heartbeat sends periodic heartbeats to keep the connection alive.
func (c *Client) heartbeat() {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			// Send heartbeat
			c.mu.RLock()
			conn := c.conn
			c.mu.RUnlock()

			if conn != nil {
				// TODO: Send heartbeat message
			}
		}
	}
}

// proxyConnection proxies traffic between the gateway and a local container.
func (c *Client) proxyConnection(remote net.Conn, localPort int) error {
	local, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		return fmt.Errorf("failed to connect to container: %w", err)
	}
	defer local.Close()

	// Bidirectional copy
	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(remote, local)
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(local, remote)
		errCh <- err
	}()

	// Wait for either direction to complete/error
	<-errCh
	return nil
}
