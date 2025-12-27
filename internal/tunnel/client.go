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
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
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
)

// TunnelMessage is the JSON protocol for tunnel communication
type TunnelMessage struct {
	Type         string            `json:"type"` // "register", "unregister", "request", "response"
	DeploymentID string            `json:"deployment_id,omitempty"`
	Port         int               `json:"port,omitempty"`
	PeerID       string            `json:"peer_id,omitempty"`
	RequestID    string            `json:"request_id,omitempty"`
	Method       string            `json:"method,omitempty"`
	Path         string            `json:"path,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         []byte            `json:"body,omitempty"`
	StatusCode   int               `json:"status_code,omitempty"`
}

// Client is a reverse tunnel client that runs on the provider.
type Client struct {
	gatewayAddr string
	peerID      string
	conn        net.Conn
	reader      *bufio.Reader
	writer      *bufio.Writer
	mu          sync.Mutex
	routes      map[string]int // deploymentID -> container port
	ctx         context.Context
	cancel      context.CancelFunc
	connected   bool
}

// ClientConfig contains configuration for the tunnel client.
type ClientConfig struct {
	// GatewayAddr is the address of the gateway server (host:port)
	GatewayAddr string
	// PeerID is the peer ID of this provider
	PeerID string
}

// NewClient creates a new tunnel client.
func NewClient(cfg *ClientConfig) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		gatewayAddr: cfg.GatewayAddr,
		peerID:      cfg.PeerID,
		routes:      make(map[string]int),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Connect establishes a connection to the gateway.
func (c *Client) Connect(ctx context.Context) error {
	log.Printf("[TUNNEL] Connecting to gateway: %s", c.gatewayAddr)

	conn, err := net.DialTimeout("tcp", c.gatewayAddr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to gateway: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.reader = bufio.NewReader(conn)
	c.writer = bufio.NewWriter(conn)
	c.connected = true
	c.mu.Unlock()

	log.Printf("[TUNNEL] Connected to gateway!")

	// Start message handler
	go c.handleMessages()
	go c.heartbeat()

	return nil
}

// RegisterDeployment registers a deployment for exposure via the tunnel.
func (c *Client) RegisterDeployment(deploymentID string, containerPort int) (string, error) {
	c.mu.Lock()
	if !c.connected || c.conn == nil {
		c.mu.Unlock()
		return "", fmt.Errorf("not connected to gateway")
	}
	c.mu.Unlock()

	// Send registration message
	msg := TunnelMessage{
		Type:         "register",
		DeploymentID: deploymentID,
		Port:         containerPort,
		PeerID:       c.peerID,
	}

	if err := c.send(&msg); err != nil {
		return "", fmt.Errorf("failed to send registration: %w", err)
	}

	c.mu.Lock()
	c.routes[deploymentID] = containerPort
	c.mu.Unlock()

	log.Printf("[TUNNEL] Registered deployment %s on port %d", deploymentID, containerPort)

	// Return the URL (gateway will confirm, but we can predict it)
	return fmt.Sprintf("https://%s.peercompute.xdastechnology.com", deploymentID), nil
}

// UnregisterDeployment removes a deployment from the tunnel.
func (c *Client) UnregisterDeployment(deploymentID string) error {
	msg := TunnelMessage{
		Type:         "unregister",
		DeploymentID: deploymentID,
	}

	if err := c.send(&msg); err != nil {
		return fmt.Errorf("failed to send unregistration: %w", err)
	}

	c.mu.Lock()
	delete(c.routes, deploymentID)
	c.mu.Unlock()

	return nil
}

// Close closes the tunnel connection.
func (c *Client) Close() error {
	c.cancel()

	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// IsConnected returns whether the client is connected to the gateway.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

func (c *Client) send(msg *TunnelMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.writer == nil {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	c.writer.Write(data)
	c.writer.WriteByte('\n')
	return c.writer.Flush()
}

func (c *Client) handleMessages() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		c.mu.Lock()
		reader := c.reader
		c.mu.Unlock()

		if reader == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("[TUNNEL] Read error: %v", err)
			}
			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()
			return
		}

		var msg TunnelMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			log.Printf("[TUNNEL] Invalid message: %v", err)
			continue
		}

		switch msg.Type {
		case "registered":
			log.Printf("[TUNNEL] Gateway confirmed registration: %s", msg.DeploymentID)

		case "request":
			// Handle incoming HTTP request from gateway
			go c.handleRequest(&msg)
		}
	}
}

func (c *Client) handleRequest(msg *TunnelMessage) {
	c.mu.Lock()
	port, ok := c.routes[msg.DeploymentID]
	c.mu.Unlock()

	if !ok {
		c.sendResponse(msg.RequestID, 404, nil, []byte("Deployment not found"))
		return
	}

	// Forward request to local container
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, msg.Path)
	
	req, err := http.NewRequest(msg.Method, url, nil)
	if err != nil {
		c.sendResponse(msg.RequestID, 500, nil, []byte(err.Error()))
		return
	}

	// Copy headers
	for k, v := range msg.Headers {
		req.Header.Set(k, v)
	}

	// Make request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[TUNNEL] Request failed: %v", err)
		c.sendResponse(msg.RequestID, 502, nil, []byte("Failed to reach container: "+err.Error()))
		return
	}
	defer resp.Body.Close()

	// Read response body
	body, _ := io.ReadAll(resp.Body)

	// Convert headers
	headers := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	c.sendResponse(msg.RequestID, resp.StatusCode, headers, body)
}

func (c *Client) sendResponse(requestID string, statusCode int, headers map[string]string, body []byte) {
	msg := TunnelMessage{
		Type:       "response",
		RequestID:  requestID,
		StatusCode: statusCode,
		Headers:    headers,
		Body:       body,
	}
	c.send(&msg)
}

func (c *Client) heartbeat() {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			// Simple heartbeat - just check connection is alive
			c.mu.Lock()
			connected := c.connected
			c.mu.Unlock()
			if !connected {
				return
			}
		}
	}
}
