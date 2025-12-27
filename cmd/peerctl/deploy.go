package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spf13/cobra"
	"github.com/xdas-research/peer-compute/internal/identity"
	"github.com/xdas-research/peer-compute/internal/p2p"
	"github.com/xdas-research/peer-compute/internal/protocol"
)

func newDeployCmd() *cobra.Command {
	var (
		peerName   string
		cpu        string
		memory     string
		exposePort int
		envVars    []string
		timeout    time.Duration
	)

	cmd := &cobra.Command{
		Use:   "deploy <image>",
		Short: "Deploy a container to a peer",
		Long: `Deploy a Docker container to a trusted peer.

The container will be pulled and started on the peer machine with the specified
resource limits. If --expose is specified, the container will be accessible
via a public URL through the gateway.

Examples:
  peerctl deploy nginx:alpine --peer alice --cpu 0.5 --memory 256M --expose 80
  peerctl deploy my-api:latest --peer bob --cpu 1 --memory 512M
  peerctl deploy redis:7 --peer alice --cpu 0.25 --memory 128M`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			imageName := args[0]

			if peerName == "" {
				return fmt.Errorf("--peer is required")
			}

			// Parse resource limits
			cpuMillicores, err := parseCPU(cpu)
			if err != nil {
				return fmt.Errorf("invalid CPU value: %w", err)
			}

			memoryBytes, err := parseMemory(memory)
			if err != nil {
				return fmt.Errorf("invalid memory value: %w", err)
			}

			// Parse environment variables
			env, err := parseEnvVars(envVars)
			if err != nil {
				return fmt.Errorf("invalid environment variable: %w", err)
			}

			// Load identity
			id, _, err := identity.LoadOrGenerate(identity.DefaultKeyPath())
			if err != nil {
				return fmt.Errorf("failed to load identity: %w", err)
			}

			// Find target peer
			trustPath := identity.DefaultTrustedPeersPath()
			tm := p2p.NewTrustManager(trustPath)
			if err := tm.Load(); err != nil {
				return fmt.Errorf("failed to load trust list: %w", err)
			}

			targetPeer, err := findPeerByName(tm, peerName)
			if err != nil {
				return err
			}

			// Create deployment request
			req := &protocol.DeployRequest{
				RequestID:     uuid.New().String(),
				Image:         imageName,
				CPUMillicores: cpuMillicores,
				MemoryBytes:   memoryBytes,
				ExposePort:    exposePort,
				Environment:   env,
				RequesterID:   id.PeerID.String(),
				Timestamp:     time.Now().UnixNano(),
			}

			// Sign the request
			if err := signRequest(req, id); err != nil {
				return fmt.Errorf("failed to sign request: %w", err)
			}

			fmt.Printf("Deploying %s to peer %s...\n", imageName, targetPeer.ID)
			fmt.Printf("  CPU: %d millicores\n", cpuMillicores)
			fmt.Printf("  Memory: %d bytes\n", memoryBytes)
			if exposePort > 0 {
				fmt.Printf("  Expose port: %d\n", exposePort)
			}

			// Create context with timeout
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			// Create P2P host
			host, err := p2p.NewHost(ctx, &p2p.Config{
				Identity:     id,
				ListenPort:   0, // Random port
				TrustManager: tm,
			})
			if err != nil {
				return fmt.Errorf("failed to create P2P host: %w", err)
			}
			defer host.Close()

			// Parse target peer address
			addrInfo, err := p2p.ParseAddrInfo(targetPeer.ID.String(), targetPeer.Addresses)
			if err != nil {
				return fmt.Errorf("failed to parse peer address: %w", err)
			}

			// Connect to the peer
			fmt.Print("\nConnecting to peer...")
			if err := host.Connect(ctx, addrInfo); err != nil {
				return fmt.Errorf("\nfailed to connect to peer: %w", err)
			}
			fmt.Println(" connected!")

			// Open stream and send deploy request
			fmt.Print("Sending deployment request...")
			stream, err := host.NewStream(ctx, targetPeer.ID, "/peercompute/deploy/1.0.0")
			if err != nil {
				return fmt.Errorf("\nfailed to open stream: %w", err)
			}
			defer stream.Close()

			// Send request
			encoder := json.NewEncoder(stream)
			if err := encoder.Encode(req); err != nil {
				return fmt.Errorf("\nfailed to send request: %w", err)
			}

			// Read response
			var resp protocol.DeployResponse
			decoder := json.NewDecoder(stream)
			if err := decoder.Decode(&resp); err != nil {
				return fmt.Errorf("\nfailed to read response: %w", err)
			}

			if !resp.Success {
				return fmt.Errorf("\ndeployment failed: %s", resp.Message)
			}

			fmt.Println(" success!")
			fmt.Println("\n✓ Deployment successful!")
			fmt.Printf("  Deployment ID: %s\n", resp.DeploymentID)
			if resp.ContainerID != "" {
				fmt.Printf("  Container ID: %s\n", resp.ContainerID[:12])
			}
			fmt.Println("\nUse 'peerctl logs <deployment-id>' to view logs")
			fmt.Println("Use 'peerctl stop <deployment-id>' to stop the deployment")

			return nil
		},
	}

	cmd.Flags().StringVar(&peerName, "peer", "", "Target peer (ID or name)")
	cmd.Flags().StringVar(&cpu, "cpu", "0.5", "CPU limit (e.g., 0.5, 1, 2)")
	cmd.Flags().StringVar(&memory, "memory", "256M", "Memory limit (e.g., 128M, 1G)")
	cmd.Flags().IntVar(&exposePort, "expose", 0, "Container port to expose via gateway")
	cmd.Flags().StringSliceVar(&envVars, "env", nil, "Environment variables (KEY=VALUE)")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Deployment timeout")

	cmd.MarkFlagRequired("peer")

	return cmd
}

// parseCPU parses a CPU value (e.g., "0.5", "1", "2") to millicores.
func parseCPU(s string) (int64, error) {
	var value float64
	if _, err := fmt.Sscanf(s, "%f", &value); err != nil {
		return 0, fmt.Errorf("invalid format")
	}
	if value <= 0 {
		return 0, fmt.Errorf("must be positive")
	}
	if value > 32 {
		return 0, fmt.Errorf("maximum 32 CPUs")
	}
	return int64(value * 1000), nil
}

// parseMemory parses a memory value (e.g., "256M", "1G") to bytes.
func parseMemory(s string) (int64, error) {
	var value int64
	var unit byte

	if _, err := fmt.Sscanf(s, "%d%c", &value, &unit); err != nil {
		return 0, fmt.Errorf("invalid format (use e.g., 256M or 1G)")
	}

	switch unit {
	case 'K', 'k':
		return value * 1024, nil
	case 'M', 'm':
		return value * 1024 * 1024, nil
	case 'G', 'g':
		return value * 1024 * 1024 * 1024, nil
	default:
		return 0, fmt.Errorf("unknown unit '%c' (use K, M, or G)", unit)
	}
}

// parseEnvVars parses environment variables from KEY=VALUE format.
func parseEnvVars(vars []string) (map[string]string, error) {
	result := make(map[string]string)
	for _, v := range vars {
		for i := 0; i < len(v); i++ {
			if v[i] == '=' {
				key := v[:i]
				value := v[i+1:]
				if key == "" {
					return nil, fmt.Errorf("empty key in '%s'", v)
				}
				result[key] = value
				break
			}
		}
	}
	return result, nil
}

// findPeerByName finds a peer by ID or name.
func findPeerByName(tm *p2p.TrustManager, nameOrID string) (*p2p.TrustedPeer, error) {
	peers := tm.List()

	// Try to parse as peer ID first
	if peerID, err := peer.Decode(nameOrID); err == nil {
		if p, ok := tm.Get(peerID); ok {
			return p, nil
		}
	}

	// Try to find by name
	for _, p := range peers {
		if p.Name == nameOrID {
			return p, nil
		}
	}

	return nil, fmt.Errorf("peer '%s' not found in trust list", nameOrID)
}

// signRequest signs a deployment request.
func signRequest(req *protocol.DeployRequest, id *identity.Identity) error {
	// Create signing payload
	payload := fmt.Sprintf("%s:%s:%d:%d:%d:%s:%d",
		req.RequestID,
		req.Image,
		req.CPUMillicores,
		req.MemoryBytes,
		req.ExposePort,
		req.RequesterID,
		req.Timestamp,
	)

	sig, err := id.Sign([]byte(payload))
	if err != nil {
		return err
	}

	req.Signature = sig
	return nil
}
