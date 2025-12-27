// Peer Compute Daemon (peercomputed)
//
// peercomputed is the provider agent that runs on machines offering compute
// resources. It handles deployment requests, container execution, and tunnel
// management.
//
// Key responsibilities:
// - Generate and persist cryptographic identity
// - Join P2P network and discover other peers
// - Accept deployment requests from trusted peers only
// - Execute containers with strict security isolation
// - Open reverse tunnels for exposed services
// - Stream logs and status updates
// - Auto-cleanup on exit
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/xdas-research/peer-compute/internal/identity"
	"github.com/xdas-research/peer-compute/internal/p2p"
	"github.com/xdas-research/peer-compute/internal/runtime"
	"github.com/xdas-research/peer-compute/internal/scheduler"
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

	// Create context that cancels on shutdown signals
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, shutting down...", sig)
		cancel()
	}()

	// Run the daemon
	if err := run(ctx, cfg); err != nil {
		log.Fatalf("Fatal error: %v", err)
	}

	log.Println("Shutdown complete")
}

func parseFlags() *Config {
	cfg := &Config{}

	flag.IntVar(&cfg.ListenPort, "port", 9000, "P2P listen port")
	flag.StringVar(&cfg.GatewayAddr, "gateway", "", "Gateway address for tunnel connections")
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

	// Verify Docker is available
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

	// Print listening addresses
	log.Println("Listening on:")
	for _, addr := range host.Addrs() {
		log.Printf("  %s/p2p/%s", addr, host.ID())
	}

	// 6. Register protocol handlers
	log.Println("Registering protocol handlers...")
	registerHandlers(host, sched, id)

	// 7. Start discovery
	log.Println("Starting peer discovery...")
	discovery := p2p.NewDiscovery(host.Host(), trust)
	if err := discovery.Start(ctx); err != nil {
		log.Printf("Warning: failed to start discovery: %v", err)
	}
	defer discovery.Stop()

	// 8. Connect to known peers
	go connectToKnownPeers(ctx, host, trust)

	log.Println("Peer Compute Daemon ready")
	log.Printf("Peer ID: %s", id.PeerID)

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

func registerHandlers(host *p2p.Host, sched *scheduler.Scheduler, id *identity.Identity) {
	// Deploy handler
	host.SetStreamHandler("/peercompute/deploy/1.0.0", func(stream network.Stream) {
		// Handle deployment request
		// This would:
		// 1. Read and verify the request
		// 2. Check if requester is trusted
		// 3. Validate resources
		// 4. Schedule the deployment
		// 5. Send response
		defer stream.Close()
	})

	// Logs handler
	host.SetStreamHandler("/peercompute/logs/1.0.0", func(stream network.Stream) {
		// Handle log streaming request
		defer stream.Close()
	})

	// Status handler
	host.SetStreamHandler("/peercompute/status/1.0.0", func(stream network.Stream) {
		// Handle status request
		defer stream.Close()
	})

	// Stop handler
	host.SetStreamHandler("/peercompute/stop/1.0.0", func(stream network.Stream) {
		// Handle stop request
		defer stream.Close()
	})
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
