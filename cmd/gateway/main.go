// Peer Compute Gateway
//
// The gateway is a public service that enables external access to containers
// deployed on provider peers. It:
// - Accepts reverse tunnel connections from providers
// - Terminates TLS for public traffic
// - Routes requests to the appropriate container via tunnels
// - Manages subdomain allocation
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

var (
	Version = "dev"
	Commit  = "unknown"
)

type Config struct {
	HTTPPort       int
	HTTPSPort      int
	TunnelPort     int
	BaseDomain     string
	ACMEEmail      string
	DataDir        string
	DisableTLS     bool
	RateLimitRPS   int
	IdleTimeout    time.Duration
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
}

func main() {
	cfg := parseFlags()

	log.Printf("Peer Compute Gateway %s (commit: %s)", Version, Commit)
	log.Printf("Starting gateway...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, shutting down...", sig)
		cancel()
	}()

	if err := run(ctx, cfg); err != nil {
		log.Fatalf("Fatal error: %v", err)
	}

	log.Println("Shutdown complete")
}

func parseFlags() *Config {
	cfg := &Config{}

	flag.IntVar(&cfg.HTTPPort, "http-port", 80, "HTTP port (for ACME challenges)")
	flag.IntVar(&cfg.HTTPSPort, "https-port", 443, "HTTPS port")
	flag.IntVar(&cfg.TunnelPort, "tunnel-port", 8443, "Tunnel connection port")
	flag.StringVar(&cfg.BaseDomain, "domain", "peercompute.xdastechnology.com", "Base domain for subdomains")
	flag.StringVar(&cfg.ACMEEmail, "acme-email", "", "Email for Let's Encrypt")
	flag.StringVar(&cfg.DataDir, "data-dir", "/var/lib/peercompute-gateway", "Data directory")
	flag.BoolVar(&cfg.DisableTLS, "disable-tls", false, "Disable TLS (for development)")
	flag.IntVar(&cfg.RateLimitRPS, "rate-limit", 100, "Rate limit requests per second")
	flag.DurationVar(&cfg.IdleTimeout, "idle-timeout", 120*time.Second, "Idle connection timeout")
	flag.DurationVar(&cfg.ReadTimeout, "read-timeout", 30*time.Second, "Read timeout")
	flag.DurationVar(&cfg.WriteTimeout, "write-timeout", 30*time.Second, "Write timeout")
	flag.Parse()

	return cfg
}

func run(ctx context.Context, cfg *Config) error {
	// Initialize tunnel server
	log.Println("Initializing tunnel server...")
	tunnelMgr := NewTunnelManager()

	// Start tunnel listener
	go func() {
		if err := tunnelMgr.ListenAndServe(fmt.Sprintf(":%d", cfg.TunnelPort), cfg.BaseDomain); err != nil {
			log.Printf("Tunnel server error: %v", err)
		}
	}()

	// Create HTTP handler
	handler := NewGatewayHandler(tunnelMgr, cfg)

	// Add middleware
	handler = withLogging(handler)
	handler = withRecovery(handler)

	var httpServer *http.Server
	var httpsServer *http.Server

	if cfg.DisableTLS {
		// Development mode - HTTP only
		httpServer = &http.Server{
			Addr:         fmt.Sprintf(":%d", cfg.HTTPPort),
			Handler:      handler,
			IdleTimeout:  cfg.IdleTimeout,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
		}

		log.Printf("Starting HTTP server on :%d (TLS disabled)", cfg.HTTPPort)
		go func() {
			if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
				log.Printf("HTTP server error: %v", err)
			}
		}()
	} else {
		// Production mode - HTTPS with Let's Encrypt
		log.Printf("Setting up Let's Encrypt for *.%s", cfg.BaseDomain)

		certManager := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(cfg.BaseDomain, "*."+cfg.BaseDomain),
			Cache:      autocert.DirCache(cfg.DataDir + "/certs"),
			Email:      cfg.ACMEEmail,
		}

		// HTTP server for ACME challenges
		httpServer = &http.Server{
			Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
			Handler: certManager.HTTPHandler(nil),
		}
		go func() {
			log.Printf("Starting HTTP server on :%d (ACME challenges)", cfg.HTTPPort)
			if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
				log.Printf("HTTP server error: %v", err)
			}
		}()

		// HTTPS server
		httpsServer = &http.Server{
			Addr:    fmt.Sprintf(":%d", cfg.HTTPSPort),
			Handler: handler,
			TLSConfig: &tls.Config{
				GetCertificate: certManager.GetCertificate,
				MinVersion:     tls.VersionTLS12,
			},
			IdleTimeout:  cfg.IdleTimeout,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
		}

		go func() {
			log.Printf("Starting HTTPS server on :%d", cfg.HTTPSPort)
			if err := httpsServer.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
				log.Printf("HTTPS server error: %v", err)
			}
		}()
	}

	log.Printf("Gateway ready. Base domain: %s", cfg.BaseDomain)
	log.Printf("Tunnel server listening on :%d", cfg.TunnelPort)

	// Wait for shutdown
	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if httpServer != nil {
		httpServer.Shutdown(shutdownCtx)
	}
	if httpsServer != nil {
		httpsServer.Shutdown(shutdownCtx)
	}
	tunnelMgr.Close()

	return nil
}

// TunnelMessage is the JSON protocol for tunnel communication
type TunnelMessage struct {
	Type         string `json:"type"` // "register", "unregister", "request", "response"
	DeploymentID string `json:"deployment_id,omitempty"`
	Port         int    `json:"port,omitempty"`
	PeerID       string `json:"peer_id,omitempty"`
	RequestID    string `json:"request_id,omitempty"`
	Method       string `json:"method,omitempty"`
	Path         string `json:"path,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         []byte `json:"body,omitempty"`
	StatusCode   int    `json:"status_code,omitempty"`
}

// TunnelManager manages reverse tunnel connections from providers.
type TunnelManager struct {
	mu       sync.RWMutex
	tunnels  map[string]*TunnelConn      // peerID -> tunnel
	routes   map[string]*TunnelConn      // deploymentID -> tunnel
	listener net.Listener
}

type TunnelConn struct {
	PeerID     string
	Conn       net.Conn
	Reader     *bufio.Reader
	Writer     *bufio.Writer
	Routes     map[string]int // deployment ID -> local port
	mu         sync.Mutex
	pending    map[string]chan *TunnelMessage // requestID -> response channel
}

func NewTunnelManager() *TunnelManager {
	return &TunnelManager{
		tunnels: make(map[string]*TunnelConn),
		routes:  make(map[string]*TunnelConn),
	}
}

func (tm *TunnelManager) ListenAndServe(addr string, baseDomain string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	tm.listener = listener
	log.Printf("Tunnel server listening on %s", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go tm.handleTunnelConn(conn, baseDomain)
	}
}

func (tm *TunnelManager) handleTunnelConn(conn net.Conn, baseDomain string) {
	log.Printf("New tunnel connection from %s", conn.RemoteAddr())
	
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	
	tc := &TunnelConn{
		Conn:    conn,
		Reader:  reader,
		Writer:  writer,
		Routes:  make(map[string]int),
		pending: make(map[string]chan *TunnelMessage),
	}

	// Read messages from provider
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("Tunnel read error: %v", err)
			}
			break
		}

		var msg TunnelMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			log.Printf("Invalid tunnel message: %v", err)
			continue
		}

		switch msg.Type {
		case "register":
			tc.PeerID = msg.PeerID
			tc.Routes[msg.DeploymentID] = msg.Port
			
			tm.mu.Lock()
			tm.tunnels[msg.PeerID] = tc
			tm.routes[msg.DeploymentID] = tc
			tm.mu.Unlock()
			
			url := fmt.Sprintf("https://%s.%s", msg.DeploymentID, baseDomain)
			log.Printf("Registered deployment %s -> port %d (URL: %s)", msg.DeploymentID, msg.Port, url)
			
			// Send confirmation
			resp := TunnelMessage{
				Type:         "registered",
				DeploymentID: msg.DeploymentID,
			}
			tc.Send(&resp)

		case "unregister":
			tm.mu.Lock()
			delete(tm.routes, msg.DeploymentID)
			delete(tc.Routes, msg.DeploymentID)
			tm.mu.Unlock()
			log.Printf("Unregistered deployment %s", msg.DeploymentID)

		case "response":
			tc.mu.Lock()
			if ch, ok := tc.pending[msg.RequestID]; ok {
				ch <- &msg
				delete(tc.pending, msg.RequestID)
			}
			tc.mu.Unlock()
		}
	}

	// Cleanup on disconnect
	tm.mu.Lock()
	if tc.PeerID != "" {
		delete(tm.tunnels, tc.PeerID)
	}
	for depID := range tc.Routes {
		delete(tm.routes, depID)
	}
	tm.mu.Unlock()
	
	conn.Close()
	log.Printf("Tunnel disconnected: %s", conn.RemoteAddr())
}

func (tc *TunnelConn) Send(msg *TunnelMessage) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	
	tc.Writer.Write(data)
	tc.Writer.WriteByte('\n')
	return tc.Writer.Flush()
}

func (tc *TunnelConn) Request(msg *TunnelMessage, timeout time.Duration) (*TunnelMessage, error) {
	ch := make(chan *TunnelMessage, 1)
	
	tc.mu.Lock()
	tc.pending[msg.RequestID] = ch
	tc.mu.Unlock()
	
	if err := tc.Send(msg); err != nil {
		tc.mu.Lock()
		delete(tc.pending, msg.RequestID)
		tc.mu.Unlock()
		return nil, err
	}
	
	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(timeout):
		tc.mu.Lock()
		delete(tc.pending, msg.RequestID)
		tc.mu.Unlock()
		return nil, fmt.Errorf("request timeout")
	}
}

func (tm *TunnelManager) Close() {
	if tm.listener != nil {
		tm.listener.Close()
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	for _, tc := range tm.tunnels {
		tc.Conn.Close()
	}
}

func (tm *TunnelManager) GetTunnel(deploymentID string) (*TunnelConn, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	tc, ok := tm.routes[deploymentID]
	return tc, ok
}

// GatewayHandler routes incoming HTTP requests to containers via tunnels.
type GatewayHandler struct {
	tunnels *TunnelManager
	cfg     *Config
}

func NewGatewayHandler(tunnels *TunnelManager, cfg *Config) http.Handler {
	return &GatewayHandler{
		tunnels: tunnels,
		cfg:     cfg,
	}
}

func (h *GatewayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract subdomain from Host header
	subdomain := extractSubdomain(r.Host, h.cfg.BaseDomain)
	if subdomain == "" {
		// No subdomain - show gateway info
		h.serveGatewayInfo(w, r)
		return
	}

	// Find the tunnel for this subdomain
	tunnel, ok := h.tunnels.GetTunnel(subdomain)
	if !ok {
		http.Error(w, fmt.Sprintf("Deployment '%s' not found", subdomain), http.StatusNotFound)
		return
	}

	// Proxy the request through the tunnel
	h.proxyRequest(w, r, tunnel, subdomain)
}

func (h *GatewayHandler) serveGatewayInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>Peer Compute Gateway</title>
    <style>
        body { font-family: -apple-system, sans-serif; max-width: 600px; margin: 50px auto; padding: 20px; background: #0a0a0a; color: #fff; }
        h1 { color: #00ff88; }
        code { background: #1a1a1a; padding: 2px 6px; border-radius: 3px; color: #00ff88; }
        a { color: #00aaff; }
    </style>
</head>
<body>
    <h1>🖥️ Peer Compute Gateway</h1>
    <p>This is the Peer Compute gateway for <code>%s</code>.</p>
    <p>Deployed containers are accessible via subdomains:</p>
    <p><code>https://&lt;deployment-id&gt;.%s</code></p>
    <p>Learn more at <a href="https://github.com/xdas-research/peer-compute">github.com/xdas-research/peer-compute</a></p>
</body>
</html>`, h.cfg.BaseDomain, h.cfg.BaseDomain)
}

func (h *GatewayHandler) proxyRequest(w http.ResponseWriter, r *http.Request, tunnel *TunnelConn, deploymentID string) {
	// Read request body
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
	}

	// Build headers map
	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	// Create request message
	requestID := fmt.Sprintf("%d", time.Now().UnixNano())
	msg := &TunnelMessage{
		Type:         "request",
		RequestID:    requestID,
		DeploymentID: deploymentID,
		Method:       r.Method,
		Path:         r.URL.RequestURI(),
		Headers:      headers,
		Body:         body,
	}

	// Send request and wait for response
	resp, err := tunnel.Request(msg, 30*time.Second)
	if err != nil {
		log.Printf("Proxy error for %s: %v", deploymentID, err)
		http.Error(w, "Gateway error: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Write response headers
	for k, v := range resp.Headers {
		w.Header().Set(k, v)
	}
	
	if resp.StatusCode > 0 {
		w.WriteHeader(resp.StatusCode)
	}
	
	if len(resp.Body) > 0 {
		w.Write(resp.Body)
	}
}

func extractSubdomain(host, baseDomain string) string {
	// Remove port if present
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	suffix := "." + baseDomain
	if strings.HasSuffix(host, suffix) {
		subdomain := host[:len(host)-len(suffix)]
		// Ignore "www" subdomain
		if subdomain != "www" && subdomain != "" {
			return subdomain
		}
	}
	return ""
}

// Middleware

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s %v", r.Method, r.Host, r.URL.Path, time.Since(start))
	})
}

func withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("Panic recovered: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
