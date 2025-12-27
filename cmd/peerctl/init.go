package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/xdas-research/peer-compute/internal/identity"
)

func newInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new Peer Compute identity",
		Long: `Initialize creates a new cryptographic identity for this peer.

The identity consists of an Ed25519 key pair. The private key is stored
locally and never transmitted over the network. The public key is used
to derive your Peer ID, which other peers use to identify you.

Your identity is stored in ~/.peercompute/identity.key`,
		RunE: func(cmd *cobra.Command, args []string) error {
			keyPath := identity.DefaultKeyPath()

			// Check if identity already exists
			if _, err := os.Stat(keyPath); err == nil && !force {
				return fmt.Errorf("identity already exists at %s (use --force to overwrite)", keyPath)
			}

			// Generate or load identity
			id, isNew, err := identity.LoadOrGenerate(keyPath)
			if err != nil {
				return fmt.Errorf("failed to initialize identity: %w", err)
			}

			if isNew || force {
				// Generate new identity
				id, err = identity.Generate()
				if err != nil {
					return fmt.Errorf("failed to generate identity: %w", err)
				}
				if err := id.Save(keyPath); err != nil {
					return fmt.Errorf("failed to save identity: %w", err)
				}
				fmt.Println("✓ Generated new identity")
			} else {
				fmt.Println("✓ Loaded existing identity")
			}

			fmt.Printf("\nYour Peer ID: %s\n", id.PeerID.String())
			fmt.Printf("Identity stored at: %s\n", keyPath)
			fmt.Println("\nShare your Peer ID with others so they can add you as a trusted peer.")

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing identity")

	return cmd
}
