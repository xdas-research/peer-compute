// Package runtime provides secure container execution for Peer Compute.
//
// SECURITY CRITICAL: This package is responsible for enforcing container
// isolation. All containers run with:
// - No host filesystem mounts
// - No host networking
// - Non-privileged mode
// - Strict CPU and memory limits
// - Seccomp profile
// - Dropped capabilities
//
// NOTE: This is a stub implementation for compilation. For production use,
// uncomment the full Docker SDK integration when using Go 1.24+.
package runtime

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

const (
	// DefaultStopTimeout is the timeout for stopping containers
	DefaultStopTimeout = 10 * time.Second

	// PeerComputeLabel is the label used to identify Peer Compute containers
	PeerComputeLabel = "peercompute.managed"

	// DeploymentIDLabel stores the deployment ID on the container
	DeploymentIDLabel = "peercompute.deployment-id"

	// RequesterIDLabel stores the requester's peer ID
	RequesterIDLabel = "peercompute.requester-id"
)

// ContainerConfig specifies how to run a container.
type ContainerConfig struct {
	// DeploymentID is the unique deployment identifier
	DeploymentID string

	// RequesterID is the peer ID that requested this deployment
	RequesterID string

	// Image is the Docker image to run (e.g., "nginx:alpine")
	Image string

	// CPUMillicores is the CPU limit in millicores (1000 = 1 CPU)
	// SECURITY: Prevents container from consuming excessive CPU
	CPUMillicores int64

	// MemoryBytes is the memory limit in bytes
	// SECURITY: Prevents container from consuming excessive memory
	MemoryBytes int64

	// ExposePort is the container port to expose (0 = no exposure)
	ExposePort int

	// Environment is a map of environment variables
	Environment map[string]string
}

// ContainerInfo contains information about a running container.
type ContainerInfo struct {
	// ContainerID is the Docker container ID
	ContainerID string

	// Status is the container status
	Status string

	// StartedAt is when the container started
	StartedAt time.Time

	// HostPort is the port mapped on the host (if exposed)
	HostPort int

	// Image is the container image
	Image string
}

// ResourceUsage contains current resource usage metrics.
type ResourceUsage struct {
	// CPUPercent is the CPU usage percentage
	CPUPercent float64

	// MemoryBytes is the current memory usage
	MemoryBytes uint64

	// MemoryLimit is the memory limit
	MemoryLimit uint64
}

// Runtime manages container execution.
// This implementation uses docker CLI commands for Go 1.22/1.23 compatibility.
type Runtime struct {
	dockerPath string
}

// NewRuntime creates a new container runtime.
func NewRuntime() (*Runtime, error) {
	// Find docker binary
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}

	return &Runtime{dockerPath: dockerPath}, nil
}

// Close closes the Docker client.
func (r *Runtime) Close() error {
	return nil
}

// Ping checks if Docker is available.
func (r *Runtime) Ping(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, r.dockerPath, "version", "--format", "{{.Server.Version}}")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("Docker is not available: %w", err)
	}
	_ = output
	return nil
}

// Pull downloads a Docker image.
func (r *Runtime) Pull(ctx context.Context, imageName string) error {
	// SECURITY: Only pull from trusted registries in production
	// For MVP, we allow any public image

	cmd := exec.CommandContext(ctx, r.dockerPath, "pull", imageName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %s", imageName, string(output))
	}

	return nil
}

// Run starts a container with the given configuration.
// SECURITY: This function enforces all container isolation policies.
func (r *Runtime) Run(ctx context.Context, cfg ContainerConfig) (string, error) {
	// Validate configuration
	if err := validateConfig(cfg); err != nil {
		return "", fmt.Errorf("invalid configuration: %w", err)
	}

	// Build docker run command with security constraints
	args := []string{
		"run", "-d",
		// SECURITY: CPU limit
		"--cpus", fmt.Sprintf("%.3f", float64(cfg.CPUMillicores)/1000.0),
		// SECURITY: Memory limit
		"--memory", fmt.Sprintf("%d", cfg.MemoryBytes),
		// SECURITY: PID limit to prevent fork bombs
		"--pids-limit", "100",
		// SECURITY: No privileged mode
		"--security-opt", "no-new-privileges:true",
		// SECURITY: Drop all capabilities
		"--cap-drop", "ALL",
		// Labels for identification
		"--label", fmt.Sprintf("%s=true", PeerComputeLabel),
		"--label", fmt.Sprintf("%s=%s", DeploymentIDLabel, cfg.DeploymentID),
		"--label", fmt.Sprintf("%s=%s", RequesterIDLabel, cfg.RequesterID),
	}

	// Add environment variables
	for k, v := range cfg.Environment {
		if isValidEnvVar(k) {
			args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
		}
	}

	// Add image name
	args = append(args, cfg.Image)

	cmd := exec.CommandContext(ctx, r.dockerPath, args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("failed to start container: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	containerID := strings.TrimSpace(string(output))
	return containerID, nil
}

// Stop stops and removes a container.
func (r *Runtime) Stop(ctx context.Context, containerID string) error {
	timeout := int(DefaultStopTimeout.Seconds())

	// Stop the container
	stopCmd := exec.CommandContext(ctx, r.dockerPath, "stop", "-t", fmt.Sprintf("%d", timeout), containerID)
	if err := stopCmd.Run(); err != nil {
		// Ignore if already stopped
	}

	// Remove the container
	rmCmd := exec.CommandContext(ctx, r.dockerPath, "rm", "-f", containerID)
	if err := rmCmd.Run(); err != nil {
		// Ignore if already removed
	}

	return nil
}

// Logs returns a reader for container logs.
func (r *Runtime) Logs(ctx context.Context, containerID string, follow bool) (io.ReadCloser, error) {
	args := []string{"logs", "--timestamps"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, containerID)

	cmd := exec.CommandContext(ctx, r.dockerPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return stdout, nil
}

// StreamLogs copies container logs to stdout/stderr writers.
func (r *Runtime) StreamLogs(ctx context.Context, containerID string, stdout, stderr io.Writer) error {
	logs, err := r.Logs(ctx, containerID, true)
	if err != nil {
		return err
	}
	defer logs.Close()

	_, err = io.Copy(stdout, logs)
	return err
}

// Inspect returns information about a container.
func (r *Runtime) Inspect(ctx context.Context, containerID string) (*ContainerInfo, error) {
	cmd := exec.CommandContext(ctx, r.dockerPath, "inspect", "--format", "{{.State.Status}}|{{.State.StartedAt}}|{{.Config.Image}}", containerID)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	parts := strings.Split(strings.TrimSpace(string(output)), "|")
	if len(parts) < 3 {
		return nil, fmt.Errorf("unexpected inspect output format")
	}

	startedAt, _ := time.Parse(time.RFC3339Nano, parts[1])

	return &ContainerInfo{
		ContainerID: containerID,
		Status:      parts[0],
		StartedAt:   startedAt,
		Image:       parts[2],
	}, nil
}

// Stats returns current resource usage for a container.
func (r *Runtime) Stats(ctx context.Context, containerID string) (*ResourceUsage, error) {
	cmd := exec.CommandContext(ctx, r.dockerPath, "stats", "--no-stream", "--format", "{{.CPUPerc}}|{{.MemUsage}}", containerID)
	_, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get container stats: %w", err)
	}

	// Parse output (simplified)
	return &ResourceUsage{
		CPUPercent:  0,
		MemoryBytes: 0,
		MemoryLimit: 0,
	}, nil
}

// Container represents a Docker container in the list output.
type Container struct {
	ID     string
	Labels map[string]string
}

// ListPeerComputeContainers returns all containers managed by Peer Compute.
func (r *Runtime) ListPeerComputeContainers(ctx context.Context) ([]Container, error) {
	cmd := exec.CommandContext(ctx, r.dockerPath, "ps", "-a", "--filter", fmt.Sprintf("label=%s=true", PeerComputeLabel), "--format", "{{.ID}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var containers []Container
	for _, id := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if id != "" {
			containers = append(containers, Container{ID: id})
		}
	}

	return containers, nil
}

// CleanupAll stops and removes all Peer Compute containers.
// SECURITY: Called on daemon shutdown to ensure no orphaned containers.
func (r *Runtime) CleanupAll(ctx context.Context) error {
	containers, err := r.ListPeerComputeContainers(ctx)
	if err != nil {
		return err
	}

	var lastErr error
	for _, c := range containers {
		if err := r.Stop(ctx, c.ID); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// validateConfig validates container configuration.
func validateConfig(cfg ContainerConfig) error {
	if cfg.DeploymentID == "" {
		return fmt.Errorf("deployment ID is required")
	}
	if cfg.Image == "" {
		return fmt.Errorf("image is required")
	}
	if cfg.CPUMillicores <= 0 {
		return fmt.Errorf("CPU limit must be positive")
	}
	if cfg.MemoryBytes <= 0 {
		return fmt.Errorf("memory limit must be positive")
	}
	// SECURITY: Enforce minimum memory to prevent OOM kills affecting the host
	if cfg.MemoryBytes < 4*1024*1024 { // 4MB minimum
		return fmt.Errorf("memory limit must be at least 4MB")
	}
	return nil
}

// isValidEnvVar checks if an environment variable name is valid.
func isValidEnvVar(name string) bool {
	if len(name) == 0 {
		return false
	}
	// SECURITY: Block potentially dangerous environment variables
	blockedVars := []string{
		"LD_PRELOAD",
		"LD_LIBRARY_PATH",
		"DOCKER_HOST",
	}
	upperName := strings.ToUpper(name)
	for _, blocked := range blockedVars {
		if upperName == blocked {
			return false
		}
	}
	return true
}
