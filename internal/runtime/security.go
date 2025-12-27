// Package runtime - Security policy enforcement
package runtime

import (
	"fmt"
)

// SecurityPolicy defines the security constraints for container execution.
// All policies are enforced by the Runtime.Run function.
type SecurityPolicy struct {
	// AllowPrivileged allows privileged containers (NEVER set to true)
	AllowPrivileged bool

	// AllowHostNetwork allows host network access (NEVER set to true)
	AllowHostNetwork bool

	// AllowHostMounts allows host filesystem mounts (NEVER set to true)
	AllowHostMounts bool

	// MaxCPUMillicores is the maximum CPU allocation per container
	MaxCPUMillicores int64

	// MaxMemoryBytes is the maximum memory allocation per container
	MaxMemoryBytes int64

	// MaxContainers is the maximum number of concurrent containers
	MaxContainers int

	// AllowedRegistries is the list of allowed Docker registries
	// Empty list means all registries are allowed (MVP mode)
	AllowedRegistries []string
}

// DefaultSecurityPolicy returns the default security policy.
// SECURITY: These defaults are intentionally restrictive.
func DefaultSecurityPolicy() *SecurityPolicy {
	return &SecurityPolicy{
		// SECURITY: Never allow privileged mode
		AllowPrivileged: false,
		// SECURITY: Never allow host network
		AllowHostNetwork: false,
		// SECURITY: Never allow host mounts
		AllowHostMounts: false,
		// Default max 4 CPUs per container
		MaxCPUMillicores: 4000,
		// Default max 4GB memory per container
		MaxMemoryBytes: 4 * 1024 * 1024 * 1024,
		// Default max 10 concurrent containers
		MaxContainers: 10,
		// Empty = allow all registries (MVP)
		AllowedRegistries: []string{},
	}
}

// Validate checks if a container configuration complies with the security policy.
func (p *SecurityPolicy) Validate(cfg ContainerConfig) error {
	if cfg.CPUMillicores > p.MaxCPUMillicores {
		return fmt.Errorf("CPU limit %d exceeds maximum %d", cfg.CPUMillicores, p.MaxCPUMillicores)
	}

	if cfg.MemoryBytes > p.MaxMemoryBytes {
		return fmt.Errorf("memory limit %d exceeds maximum %d", cfg.MemoryBytes, p.MaxMemoryBytes)
	}

	// Check registry restrictions
	if len(p.AllowedRegistries) > 0 {
		allowed := false
		for _, registry := range p.AllowedRegistries {
			if registryMatches(cfg.Image, registry) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("image registry not in allowed list")
		}
	}

	return nil
}

// registryMatches checks if an image is from a specific registry.
func registryMatches(image, registry string) bool {
	// Simple prefix match for MVP
	// In production, use proper image name parsing
	if registry == "" || registry == "docker.io" {
		// Docker Hub images may not have a registry prefix
		return true
	}
	return len(image) >= len(registry) && image[:len(registry)] == registry
}

// SeccompProfile returns the default seccomp profile for containers.
// SECURITY: This restricts dangerous system calls.
func SeccompProfile() string {
	// Use Docker's default seccomp profile
	// In production, consider a custom, more restrictive profile
	return "default"
}

// CapabilitiesToDrop returns the list of capabilities to drop.
// SECURITY: We drop all capabilities and only add back what's needed.
func CapabilitiesToDrop() []string {
	return []string{
		"ALL",
	}
}

// CapabilitiesToAdd returns the list of capabilities to add.
// SECURITY: Only add capabilities that are absolutely necessary.
func CapabilitiesToAdd() []string {
	// For most containers, no additional capabilities are needed
	return []string{}
}
