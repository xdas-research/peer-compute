package main

import (
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spf13/cobra"
	"github.com/xdas-research/peer-compute/internal/identity"
	"github.com/xdas-research/peer-compute/internal/p2p"
)

func newPeersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "peers",
		Short: "Manage trusted peers",
		Long:  `Manage the list of peers you trust to deploy containers to or receive deployments from.`,
	}

	cmd.AddCommand(
		newPeersAddCmd(),
		newPeersRemoveCmd(),
		newPeersListCmd(),
	)

	return cmd
}

func newPeersAddCmd() *cobra.Command {
	var name string
	var addrs []string

	cmd := &cobra.Command{
		Use:   "add <peer-id>",
		Short: "Add a trusted peer",
		Long: `Add a peer to your trust list.

After adding a peer, you can deploy containers to them and they can deploy
containers to you. Trust is mutual - both peers must add each other.

Example:
  peerctl peers add 12D3KooWRq3bMEaFjZ... --name "alice" --addr "/ip4/192.168.1.100/tcp/9000"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			peerIDStr := args[0]

			// Parse peer ID
			peerID, err := peer.Decode(peerIDStr)
			if err != nil {
				return fmt.Errorf("invalid peer ID: %w", err)
			}

			// Load trust manager
			trustPath := identity.DefaultTrustedPeersPath()
			tm := p2p.NewTrustManager(trustPath)
			if err := tm.Load(); err != nil {
				return fmt.Errorf("failed to load trust list: %w", err)
			}

			// Add the peer
			if err := tm.Add(peerID, name, addrs); err != nil {
				return fmt.Errorf("failed to add peer: %w", err)
			}

			fmt.Printf("✓ Added trusted peer: %s\n", peerID)
			if name != "" {
				fmt.Printf("  Name: %s\n", name)
			}
			if len(addrs) > 0 {
				fmt.Printf("  Addresses: %s\n", strings.Join(addrs, ", "))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Human-readable name for the peer")
	cmd.Flags().StringSliceVar(&addrs, "addr", nil, "Known addresses for the peer (can specify multiple)")

	return cmd
}

func newPeersRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <peer-id>",
		Short: "Remove a trusted peer",
		Long: `Remove a peer from your trust list.

After removal, you will no longer be able to deploy to this peer, and they
will not be able to deploy to you.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			peerIDStr := args[0]

			// Parse peer ID
			peerID, err := peer.Decode(peerIDStr)
			if err != nil {
				return fmt.Errorf("invalid peer ID: %w", err)
			}

			// Load trust manager
			trustPath := identity.DefaultTrustedPeersPath()
			tm := p2p.NewTrustManager(trustPath)
			if err := tm.Load(); err != nil {
				return fmt.Errorf("failed to load trust list: %w", err)
			}

			// Remove the peer
			if err := tm.Remove(peerID); err != nil {
				return fmt.Errorf("failed to remove peer: %w", err)
			}

			fmt.Printf("✓ Removed peer: %s\n", peerID)

			return nil
		},
	}

	return cmd
}

func newPeersListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List trusted peers",
		Long:  `List all peers in your trust list.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load trust manager
			trustPath := identity.DefaultTrustedPeersPath()
			tm := p2p.NewTrustManager(trustPath)
			if err := tm.Load(); err != nil {
				return fmt.Errorf("failed to load trust list: %w", err)
			}

			peers := tm.List()
			if len(peers) == 0 {
				fmt.Println("No trusted peers. Use 'peerctl peers add <peer-id>' to add one.")
				return nil
			}

			fmt.Printf("Trusted Peers (%d):\n\n", len(peers))
			for _, p := range peers {
				fmt.Printf("  ID:    %s\n", p.ID)
				if p.Name != "" {
					fmt.Printf("  Name:  %s\n", p.Name)
				}
				if len(p.Addresses) > 0 {
					fmt.Printf("  Addrs: %s\n", strings.Join(p.Addresses, ", "))
				}
				fmt.Printf("  Added: %s\n", p.AddedAt.Format("2006-01-02 15:04:05"))
				fmt.Println()
			}

			return nil
		},
	}

	return cmd
}
