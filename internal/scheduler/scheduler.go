// Package scheduler manages deployment lifecycle on a provider peer.
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xdas-research/peer-compute/internal/protocol"
	"github.com/xdas-research/peer-compute/internal/runtime"
)

// Scheduler manages active deployments on a provider peer.
type Scheduler struct {
	runtime     *runtime.Runtime
	deployments map[string]*protocol.Deployment
	mu          sync.RWMutex
	maxSlots    int
	usedCPU     int64 // millicores
	usedMemory  int64 // bytes
	maxCPU      int64
	maxMemory   int64
}

// Config contains scheduler configuration.
type Config struct {
	// MaxDeployments is the maximum number of concurrent deployments
	MaxDeployments int
	// MaxCPU is the total CPU budget in millicores
	MaxCPU int64
	// MaxMemory is the total memory budget in bytes
	MaxMemory int64
}

// DefaultConfig returns default scheduler configuration.
func DefaultConfig() *Config {
	return &Config{
		MaxDeployments: 10,
		MaxCPU:         4000,                  // 4 CPUs
		MaxMemory:      4 * 1024 * 1024 * 1024, // 4GB
	}
}

// NewScheduler creates a new deployment scheduler.
func NewScheduler(rt *runtime.Runtime, cfg *Config) *Scheduler {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Scheduler{
		runtime:     rt,
		deployments: make(map[string]*protocol.Deployment),
		maxSlots:    cfg.MaxDeployments,
		maxCPU:      cfg.MaxCPU,
		maxMemory:   cfg.MaxMemory,
	}
}

// CanSchedule checks if a deployment can be scheduled with the given resources.
func (s *Scheduler) CanSchedule(cpuMillicores, memoryBytes int64) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.deployments) >= s.maxSlots {
		return fmt.Errorf("maximum deployment slots (%d) reached", s.maxSlots)
	}

	if s.usedCPU+cpuMillicores > s.maxCPU {
		return fmt.Errorf("insufficient CPU: need %d, available %d",
			cpuMillicores, s.maxCPU-s.usedCPU)
	}

	if s.usedMemory+memoryBytes > s.maxMemory {
		return fmt.Errorf("insufficient memory: need %d, available %d",
			memoryBytes, s.maxMemory-s.usedMemory)
	}

	return nil
}

// Schedule creates and starts a new deployment.
func (s *Scheduler) Schedule(ctx context.Context, req *protocol.DeployRequest) (*protocol.Deployment, error) {
	// Check resources
	if err := s.CanSchedule(req.CPUMillicores, req.MemoryBytes); err != nil {
		return nil, err
	}

	// Generate deployment ID
	deploymentID := generateDeploymentID()

	// Create deployment record
	deployment := &protocol.Deployment{
		ID:          deploymentID,
		Image:       req.Image,
		RequesterID: req.RequesterID,
		Status:      protocol.StatusPending,
		CPULimit:    req.CPUMillicores,
		MemoryLimit: req.MemoryBytes,
		ExposePort:  req.ExposePort,
		StartedAt:   time.Now(),
	}

	// Register the deployment
	s.mu.Lock()
	s.deployments[deploymentID] = deployment
	s.usedCPU += req.CPUMillicores
	s.usedMemory += req.MemoryBytes
	s.mu.Unlock()

	// Update status to pulling
	s.updateStatus(deploymentID, protocol.StatusPulling)

	// Pull the image
	if err := s.runtime.Pull(ctx, req.Image); err != nil {
		s.failDeployment(deploymentID, fmt.Errorf("failed to pull image: %w", err))
		return nil, err
	}

	// Update status to starting
	s.updateStatus(deploymentID, protocol.StatusStarting)

	// Start the container
	containerID, err := s.runtime.Run(ctx, runtime.ContainerConfig{
		DeploymentID:  deploymentID,
		RequesterID:   req.RequesterID,
		Image:         req.Image,
		CPUMillicores: req.CPUMillicores,
		MemoryBytes:   req.MemoryBytes,
		ExposePort:    req.ExposePort,
		Environment:   req.Environment,
	})
	if err != nil {
		s.failDeployment(deploymentID, fmt.Errorf("failed to start container: %w", err))
		return nil, err
	}

	// Update deployment with container ID
	s.mu.Lock()
	if d, ok := s.deployments[deploymentID]; ok {
		d.ContainerID = containerID
		d.Status = protocol.StatusRunning
	}
	s.mu.Unlock()

	return deployment, nil
}

// Stop stops a deployment and releases resources.
func (s *Scheduler) Stop(ctx context.Context, deploymentID string) error {
	s.mu.Lock()
	deployment, ok := s.deployments[deploymentID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("deployment %s not found", deploymentID)
	}
	deployment.Status = protocol.StatusStopping
	s.mu.Unlock()

	// Stop the container
	if deployment.ContainerID != "" {
		if err := s.runtime.Stop(ctx, deployment.ContainerID); err != nil {
			return fmt.Errorf("failed to stop container: %w", err)
		}
	}

	// Remove deployment and release resources
	s.mu.Lock()
	defer s.mu.Unlock()

	if d, ok := s.deployments[deploymentID]; ok {
		s.usedCPU -= d.CPULimit
		s.usedMemory -= d.MemoryLimit
		now := time.Now()
		d.StoppedAt = &now
		d.Status = protocol.StatusStopped
		delete(s.deployments, deploymentID)
	}

	return nil
}

// Get returns a deployment by ID.
func (s *Scheduler) Get(deploymentID string) (*protocol.Deployment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.deployments[deploymentID]
	if !ok {
		return nil, false
	}
	// Return a copy
	copy := *d
	return &copy, true
}

// List returns all active deployments.
func (s *Scheduler) List() []*protocol.Deployment {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*protocol.Deployment, 0, len(s.deployments))
	for _, d := range s.deployments {
		copy := *d
		result = append(result, &copy)
	}
	return result
}

// ListByRequester returns deployments from a specific requester.
func (s *Scheduler) ListByRequester(requesterID string) []*protocol.Deployment {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*protocol.Deployment
	for _, d := range s.deployments {
		if d.RequesterID == requesterID {
			copy := *d
			result = append(result, &copy)
		}
	}
	return result
}

// StopAll stops all deployments.
// SECURITY: Called on daemon shutdown for cleanup.
func (s *Scheduler) StopAll(ctx context.Context) []error {
	deployments := s.List()
	var errors []error

	for _, d := range deployments {
		if err := s.Stop(ctx, d.ID); err != nil {
			errors = append(errors, fmt.Errorf("failed to stop %s: %w", d.ID, err))
		}
	}

	return errors
}

// ResourceUsage returns current resource usage.
func (s *Scheduler) ResourceUsage() (cpuUsed, cpuTotal, memUsed, memTotal int64, slots, maxSlots int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.usedCPU, s.maxCPU, s.usedMemory, s.maxMemory, len(s.deployments), s.maxSlots
}

// updateStatus updates the status of a deployment.
func (s *Scheduler) updateStatus(deploymentID string, status protocol.DeploymentStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.deployments[deploymentID]; ok {
		d.Status = status
	}
}

// failDeployment marks a deployment as failed and releases resources.
func (s *Scheduler) failDeployment(deploymentID string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if d, ok := s.deployments[deploymentID]; ok {
		d.Status = protocol.StatusFailed
		s.usedCPU -= d.CPULimit
		s.usedMemory -= d.MemoryLimit
		now := time.Now()
		d.StoppedAt = &now
		delete(s.deployments, deploymentID)
	}
}

// generateDeploymentID generates a unique deployment ID.
func generateDeploymentID() string {
	return fmt.Sprintf("dep-%d", time.Now().UnixNano())
}
