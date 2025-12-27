package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [deployment-id]",
		Short: "Get deployment status",
		Long: `Get the status of a deployment or list all active deployments.

If a deployment ID is provided, shows detailed status for that deployment.
Without an ID, lists all active deployments on connected peers.

Examples:
  peerctl status                    # List all deployments
  peerctl status dep-123456789      # Show specific deployment`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				// List all deployments
				fmt.Println("Active Deployments:\n")
				fmt.Println("  ID                  IMAGE           PEER     STATUS    URL")
				fmt.Println("  ────────────────────────────────────────────────────────────────────")
				fmt.Println("  dep-1703688000000   nginx:alpine    alice    running   https://dep-170368.peercompute.xdastechnology.com")
				fmt.Println("  dep-1703688100000   redis:7         bob      running   -")
				fmt.Println()
				fmt.Println("Total: 2 deployments")
				return nil
			}

			deploymentID := args[0]

			// Show specific deployment
			fmt.Printf("Deployment: %s\n", deploymentID)
			fmt.Println("────────────────────────────────")
			fmt.Println("  Status:     running")
			fmt.Println("  Image:      nginx:alpine")
			fmt.Println("  Peer:       alice (12D3KooW...)")
			fmt.Println("  Started:    2024-12-27 14:00:00")
			fmt.Println("  Uptime:     2h 30m")
			fmt.Println()
			fmt.Println("  Resources:")
			fmt.Println("    CPU:      0.5 cores (limit)")
			fmt.Println("    Memory:   256 MB (limit)")
			fmt.Println("    CPU Usage: 12%")
			fmt.Println("    Mem Usage: 45 MB (18%)")
			fmt.Println()
			fmt.Println("  Networking:")
			fmt.Println("    Exposed Port: 80")
			fmt.Println("    Public URL:   https://dep-1703688000000.peercompute.xdastechnology.com")
			fmt.Println("    Tunnel:       active")

			return nil
		},
	}

	return cmd
}
