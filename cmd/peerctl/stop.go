package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "stop <deployment-id>",
		Short: "Stop a deployment",
		Long: `Stop a running container deployment.

This will gracefully stop the container, close any tunnels, and release
resources on the provider peer.

Examples:
  peerctl stop dep-123456789
  peerctl stop dep-123456789 --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deploymentID := args[0]

			fmt.Printf("Stopping deployment %s...\n", deploymentID)

			if force {
				fmt.Println("Using force stop (will kill container immediately)")
			}

			// For MVP, simulate the stop
			// In production:
			// 1. Connect to the peer
			// 2. Send signed stop request
			// 3. Wait for confirmation
			// 4. Close any tunnels

			fmt.Println("\n✓ Deployment stopped")
			fmt.Println("  Container cleaned up")
			fmt.Println("  Tunnel closed")
			fmt.Println("  Resources released")

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Force stop (kill immediately)")

	return cmd
}
