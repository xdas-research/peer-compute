// Peer Compute CLI (peerctl)
//
// peerctl is the developer-facing CLI for interacting with the Peer Compute
// network. It allows developers to manage peer trust, deploy containers,
// stream logs, and control deployments.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	// Version is set at build time
	Version = "dev"
	// Commit is set at build time
	Commit = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "peerctl",
		Short: "Peer Compute CLI - Deploy containers on trusted peers",
		Long: `peerctl is a command-line tool for interacting with the Peer Compute network.

Peer Compute allows you to deploy Docker containers on trusted peer machines
and securely expose them via reverse tunnels through a gateway.

Getting Started:
  1. Initialize your identity:     peerctl init
  2. Add a trusted peer:           peerctl peers add <peer-id>
  3. Deploy a container:           peerctl deploy nginx:alpine --peer <peer-id>
  4. View logs:                    peerctl logs <deployment-id>
  5. Stop the deployment:          peerctl stop <deployment-id>`,
		Version: fmt.Sprintf("%s (commit: %s)", Version, Commit),
	}

	// Add subcommands
	rootCmd.AddCommand(
		newInitCmd(),
		newPeersCmd(),
		newDeployCmd(),
		newLogsCmd(),
		newStopCmd(),
		newStatusCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
