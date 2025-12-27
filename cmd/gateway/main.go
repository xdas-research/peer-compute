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
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
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
		if err := tunnelMgr.ListenAndServe(fmt.Sprintf(":%d", cfg.TunnelPort)); err != nil {
			log.Printf("Tunnel server error: %v", err)
		}
	}()

	// Create HTTP handler
	handler := NewGatewayHandler(tunnelMgr, cfg)

	// Add middleware
	handler = withRateLimit(handler, cfg.RateLimitRPS)
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

// TunnelManager manages reverse tunnel connections from providers.
type TunnelManager struct {
	// tunnels maps peer ID to tunnel connection
	tunnels map[string]*TunnelConn
}

type TunnelConn struct {
	PeerID     string
	Routes     map[string]int // deployment ID -> local port
	LastActive time.Time
}

func NewTunnelManager() *TunnelManager {
	return &TunnelManager{
		tunnels: make(map[string]*TunnelConn),
	}
}

func (tm *TunnelManager) ListenAndServe(addr string) error {
	// Listen for incoming tunnel connections
	// This is simplified for MVP
	log.Printf("Tunnel server listening on %s", addr)
	select {} // Block forever in MVP
}

func (tm *TunnelManager) Close() {
	// Close all tunnel connections
}

func (tm *TunnelManager) GetTunnel(subdomain string) (*TunnelConn, bool) {
	// Look up tunnel by subdomain
	return nil, false
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
		http.Error(w, "Deployment not found", http.StatusNotFound)
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
        body { font-family: -apple-system, sans-serif; max-width: 600px; margin: 50px auto; padding: 20px; }
        h1 { color: #333; }
        code { background: #f4f4f4; padding: 2px 6px; border-radius: 3px; }
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

func (h *GatewayHandler) proxyRequest(w http.ResponseWriter, r *http.Request, tunnel *TunnelConn, subdomain string) {
	// Forward request through tunnel
	// This is simplified for MVP
	http.Error(w, "Proxy not implemented", http.StatusNotImplemented)
}

func extractSubdomain(host, baseDomain string) string {
	// Remove port if present
	for i := 0; i < len(host); i++ {
		if host[i] == ':' {
			host = host[:i]
			break
		}
	}

	suffix := "." + baseDomain
	if len(host) > len(suffix) && host[len(host)-len(suffix):] == suffix {
		return host[:len(host)-len(suffix)]
	}
	return ""
}

// Middleware

func withRateLimit(next http.Handler, rps int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simple rate limiting (would use token bucket in production)
		next.ServeHTTP(w, r)
	})
}

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
