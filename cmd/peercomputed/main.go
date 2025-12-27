// Peer Compute Daemon (peercomputed)
//
// peercomputed is the provider agent that runs on machines offering compute
// resources. It handles deployment requests, container execution, and tunnel
// management.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/xdas-research/peer-compute/internal/handler"
	"github.com/xdas-research/peer-compute/internal/identity"
	"github.com/xdas-research/peer-compute/internal/p2p"
	"github.com/xdas-research/peer-compute/internal/runtime"
	"github.com/xdas-research/peer-compute/internal/scheduler"
	"github.com/xdas-research/peer-compute/internal/tunnel"
)

var (
	Version = "dev"
	Commit  = "unknown"
)

type Config struct {
	ListenPort  int
	GatewayAddr string
	MaxCPU      int64
	MaxMemory   int64
	MaxDeploys  int
	DataDir     string
	Verbose     bool
}

func main() {
	cfg := parseFlags()

	log.Printf("Peer Compute Daemon %s (commit: %s)", Version, Commit)
	log.Printf("Starting peercomputed...")

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

	flag.IntVar(&cfg.ListenPort, "port", 9000, "P2P listen port")
	flag.StringVar(&cfg.GatewayAddr, "gateway", "", "Gateway address for tunnel connections (e.g., peercompute.xdastechnology.com:8443)")
	flag.Int64Var(&cfg.MaxCPU, "max-cpu", 4000, "Maximum CPU in millicores")
	flag.Int64Var(&cfg.MaxMemory, "max-memory", 4*1024*1024*1024, "Maximum memory in bytes")
	flag.IntVar(&cfg.MaxDeploys, "max-deploys", 10, "Maximum concurrent deployments")
	flag.StringVar(&cfg.DataDir, "data-dir", "", "Data directory (default: ~/.peercompute)")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Enable verbose logging")
	flag.Parse()

	if cfg.DataDir == "" {
		cfg.DataDir = identity.DefaultConfigDir()
	}

	return cfg
}

func run(ctx context.Context, cfg *Config) error {
	// 1. Initialize or load identity
	log.Println("Loading identity...")
	keyPath := cfg.DataDir + "/identity.key"
	id, isNew, err := identity.LoadOrGenerate(keyPath)
	if err != nil {
		return fmt.Errorf("failed to initialize identity: %w", err)
	}
	if isNew {
		log.Printf("Generated new identity: %s", id.PeerID)
	} else {
		log.Printf("Loaded identity: %s", id.PeerID)
	}

	// 2. Initialize Docker runtime
	log.Println("Initializing Docker runtime...")
	rt, err := runtime.NewRuntime()
	if err != nil {
		return fmt.Errorf("failed to initialize Docker runtime: %w", err)
	}
	defer rt.Close()

	if err := rt.Ping(ctx); err != nil {
		return fmt.Errorf("Docker not available: %w", err)
	}
	log.Println("Docker runtime ready")

	// 3. Initialize scheduler
	log.Println("Initializing scheduler...")
	sched := scheduler.NewScheduler(rt, &scheduler.Config{
		MaxDeployments: cfg.MaxDeploys,
		MaxCPU:         cfg.MaxCPU,
		MaxMemory:      cfg.MaxMemory,
	})

	// 4. Load trust list
	log.Println("Loading trust list...")
	trustPath := cfg.DataDir + "/trusted_peers.json"
	trust := p2p.NewTrustManager(trustPath)
	if err := trust.Load(); err != nil {
		log.Printf("Warning: failed to load trust list: %v", err)
	}
	log.Printf("Trusted peers: %d", trust.Count())

	// 5. Start P2P host
	log.Printf("Starting P2P host on port %d...", cfg.ListenPort)
	host, err := p2p.NewHost(ctx, &p2p.Config{
		Identity:     id,
		ListenPort:   cfg.ListenPort,
		TrustManager: trust,
	})
	if err != nil {
		return fmt.Errorf("failed to start P2P host: %w", err)
	}
	defer host.Close()

	log.Println("Listening on:")
	for _, addr := range host.Addrs() {
		log.Printf("  %s/p2p/%s", addr, host.ID())
	}

	// 6. Connect to gateway (if specified)
	var tunnelClient *tunnel.Client
	if cfg.GatewayAddr != "" {
		log.Printf("Connecting to gateway: %s", cfg.GatewayAddr)
		tunnelClient = tunnel.NewClient(&tunnel.ClientConfig{
			GatewayAddr: cfg.GatewayAddr,
			PeerID:      id.PeerID.String(),
		})
		if err := tunnelClient.Connect(ctx); err != nil {
			log.Printf("Warning: failed to connect to gateway: %v", err)
			log.Println("Containers will not be publicly accessible")
		} else {
			defer tunnelClient.Close()
		}
	} else {
		log.Println("No gateway specified - containers will only be accessible locally")
	}

	// 7. Register protocol handlers with tunnel support
	log.Println("Registering protocol handlers...")
	h := handler.NewHandler(sched, rt, trust, host.ID())
	h.SetTunnelClient(tunnelClient)
	h.RegisterHandlers(host)

	// 8. Start discovery
	log.Println("Starting peer discovery...")
	discovery := p2p.NewDiscovery(host.Host(), trust)
	if err := discovery.Start(ctx); err != nil {
		log.Printf("Warning: failed to start discovery: %v", err)
	}
	defer discovery.Stop()

	// 9. Connect to known peers
	go connectToKnownPeers(ctx, host, trust)

	log.Println("")
	log.Println("========================================")
	log.Println("Peer Compute Daemon ready")
	log.Printf("Peer ID: %s", id.PeerID)
	if tunnelClient != nil && tunnelClient.IsConnected() {
		log.Println("Gateway: Connected ✓")
	}
	log.Println("========================================")
	log.Println("")
	log.Println("Add this peer ID to your CLI with:")
	log.Printf("  peerctl peers add %s", id.PeerID)
	log.Println("")

	// Wait for shutdown
	<-ctx.Done()

	// Cleanup all containers
	log.Println("Cleaning up containers...")
	cleanupCtx := context.Background()
	if errs := sched.StopAll(cleanupCtx); len(errs) > 0 {
		for _, err := range errs {
			log.Printf("Cleanup error: %v", err)
		}
	}

	return nil
}

func connectToKnownPeers(ctx context.Context, host *p2p.Host, trust *p2p.TrustManager) {
	for _, peer := range trust.List() {
		if len(peer.Addresses) == 0 {
			continue
		}

		pi, err := p2p.ParseAddrInfo(peer.ID.String(), peer.Addresses)
		if err != nil {
			log.Printf("Invalid address for peer %s: %v", peer.ID, err)
			continue
		}

		if err := host.Connect(ctx, pi); err != nil {
			log.Printf("Failed to connect to peer %s: %v", peer.ID, err)
		} else {
			log.Printf("Connected to peer %s", peer.ID)
		}
	}
}
