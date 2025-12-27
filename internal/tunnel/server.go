// Package tunnel - Gateway-side tunnel server
package tunnel

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"sync"
	"time"
)

// Server is the tunnel server that runs on the gateway.
type Server struct {
	listener    net.Listener
	tlsConfig   *tls.Config
	tunnels     map[string]*ProviderTunnel
	subdomains  map[string]string // subdomain -> deploymentID
	baseDomain  string
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
}

// ProviderTunnel represents a connected provider.
type ProviderTunnel struct {
	PeerID      string
	Conn        net.Conn
	Deployments map[string]*DeploymentRoute
	LastSeen    time.Time
}

// DeploymentRoute maps a deployment to its local port on the provider.
type DeploymentRoute struct {
	DeploymentID string
	LocalPort    int
	Subdomain    string
}

// ServerConfig contains configuration for the tunnel server.
type ServerConfig struct {
	// ListenAddr is the address to listen for tunnel connections
	ListenAddr string
	// TLSConfig is the TLS configuration for incoming connections
	TLSConfig *tls.Config
	// BaseDomain is the base domain for subdomains (e.g., peercompute.xdastechnology.com)
	BaseDomain string
}

// NewServer creates a new tunnel server.
func NewServer(cfg *ServerConfig) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		tlsConfig:  cfg.TLSConfig,
		baseDomain: cfg.BaseDomain,
		tunnels:    make(map[string]*ProviderTunnel),
		subdomains: make(map[string]string),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start starts the tunnel server.
func (s *Server) Start(addr string) error {
	var listener net.Listener
	var err error

	if s.tlsConfig != nil {
		listener, err = tls.Listen("tcp", addr, s.tlsConfig)
	} else {
		listener, err = net.Listen("tcp", addr)
	}

	if err != nil {
		return fmt.Errorf("failed to start tunnel server: %w", err)
	}

	s.listener = listener

	go s.acceptLoop()

	return nil
}

// Stop stops the tunnel server.
func (s *Server) Stop() error {
	s.cancel()
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// acceptLoop accepts incoming tunnel connections.
func (s *Server) acceptLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			// Check if we're shutting down
			select {
			case <-s.ctx.Done():
				return
			default:
				continue
			}
		}

		go s.handleConnection(conn)
	}
}

// handleConnection handles a new tunnel connection from a provider.
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Read authentication and registration
	// TODO: Implement proper protocol

	// For now, we just keep the connection alive
	buf := make([]byte, 1024)
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(time.Minute))
		_, err := conn.Read(buf)
		if err != nil {
			return
		}
	}
}

// RegisterDeployment registers a deployment for routing.
func (s *Server) RegisterDeployment(peerID, deploymentID string, localPort int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tunnel, ok := s.tunnels[peerID]
	if !ok {
		return "", fmt.Errorf("provider %s not connected", peerID)
	}

	// Generate subdomain
	subdomain := generateSubdomain(deploymentID)
	fullDomain := fmt.Sprintf("%s.%s", subdomain, s.baseDomain)

	route := &DeploymentRoute{
		DeploymentID: deploymentID,
		LocalPort:    localPort,
		Subdomain:    subdomain,
	}

	tunnel.Deployments[deploymentID] = route
	s.subdomains[subdomain] = deploymentID

	return fullDomain, nil
}

// UnregisterDeployment removes a deployment from routing.
func (s *Server) UnregisterDeployment(peerID, deploymentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tunnel, ok := s.tunnels[peerID]
	if !ok {
		return fmt.Errorf("provider %s not connected", peerID)
	}

	route, ok := tunnel.Deployments[deploymentID]
	if !ok {
		return fmt.Errorf("deployment %s not found", deploymentID)
	}

	delete(s.subdomains, route.Subdomain)
	delete(tunnel.Deployments, deploymentID)

	return nil
}

// HTTPHandler returns an HTTP handler that routes requests to deployments.
func (s *Server) HTTPHandler() http.Handler {
	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// Extract subdomain from host
			subdomain := extractSubdomain(req.Host, s.baseDomain)
			if subdomain == "" {
				return
			}

			// Look up deployment
			s.mu.RLock()
			deploymentID, ok := s.subdomains[subdomain]
			s.mu.RUnlock()

			if !ok {
				return
			}

			// Find the tunnel and route
			// TODO: Get the actual backend address
			_ = deploymentID
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// Route through the tunnel
				return nil, fmt.Errorf("not implemented")
			},
		},
	}
}

// generateSubdomain generates a subdomain for a deployment.
func generateSubdomain(deploymentID string) string {
	// Use a short hash of the deployment ID
	// In production, use a proper hash function
	if len(deploymentID) > 12 {
		return deploymentID[:12]
	}
	return deploymentID
}

// extractSubdomain extracts the subdomain from a host.
func extractSubdomain(host, baseDomain string) string {
	// Remove port if present
	if idx := indexOf(host, ':'); idx >= 0 {
		host = host[:idx]
	}

	// Check if it ends with base domain
	suffix := "." + baseDomain
	if len(host) > len(suffix) && host[len(host)-len(suffix):] == suffix {
		return host[:len(host)-len(suffix)]
	}

	return ""
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// ForwardRequest forwards an HTTP request through a tunnel.
func (s *Server) ForwardRequest(deploymentID string, req *http.Request, w http.ResponseWriter) error {
	s.mu.RLock()
	// Find the tunnel and deployment
	var tunnel *ProviderTunnel
	var route *DeploymentRoute

	for _, t := range s.tunnels {
		if r, ok := t.Deployments[deploymentID]; ok {
			tunnel = t
			route = r
			break
		}
	}
	s.mu.RUnlock()

	if tunnel == nil || route == nil {
		http.Error(w, "Deployment not found", http.StatusNotFound)
		return fmt.Errorf("deployment %s not found", deploymentID)
	}

	// Forward through tunnel
	// TODO: Implement proper request forwarding
	_ = tunnel
	_ = route

	return nil
}

// HealthCheck checks if a provider tunnel is healthy.
func (s *Server) HealthCheck(peerID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tunnel, ok := s.tunnels[peerID]
	if !ok {
		return false
	}

	return time.Since(tunnel.LastSeen) < time.Minute
}

// ConnectedProviders returns the list of connected providers.
func (s *Server) ConnectedProviders() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	providers := make([]string, 0, len(s.tunnels))
	for peerID := range s.tunnels {
		providers = append(providers, peerID)
	}
	return providers
}

// proxyRequest proxies an HTTP request to a container.
func (s *Server) proxyRequest(tunnel *ProviderTunnel, route *DeploymentRoute, w http.ResponseWriter, r *http.Request) {
	// Create a connection through the tunnel
	// Write the request, read the response, and forward to the client
	// This is simplified; production would use a proper multiplexing protocol

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Forward the request through the tunnel
	// Bidirectional copy between client and tunnel
	go io.Copy(tunnel.Conn, clientConn)
	io.Copy(clientConn, tunnel.Conn)
}
