// Package handler implements P2P protocol handlers for processing
// deployment requests, log streaming, and status queries.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/xdas-research/peer-compute/internal/p2p"
	"github.com/xdas-research/peer-compute/internal/protocol"
	"github.com/xdas-research/peer-compute/internal/runtime"
	"github.com/xdas-research/peer-compute/internal/scheduler"
	"github.com/xdas-research/peer-compute/internal/tunnel"
)

// Handler processes incoming P2P protocol requests.
type Handler struct {
	scheduler    *scheduler.Scheduler
	runtime      *runtime.Runtime
	trust        *p2p.TrustManager
	peerID       peer.ID
	tunnelClient *tunnel.Client
}

// NewHandler creates a new protocol handler.
func NewHandler(sched *scheduler.Scheduler, rt *runtime.Runtime, trust *p2p.TrustManager, peerID peer.ID) *Handler {
	return &Handler{
		scheduler: sched,
		runtime:   rt,
		trust:     trust,
		peerID:    peerID,
	}
}

// SetTunnelClient sets the tunnel client for registering deployments with the gateway.
func (h *Handler) SetTunnelClient(tc *tunnel.Client) {
	h.tunnelClient = tc
}

// RegisterHandlers registers all protocol handlers on the host.
func (h *Handler) RegisterHandlers(host *p2p.Host) {
	host.SetStreamHandler("/peercompute/deploy/1.0.0", h.handleDeploy)
	host.SetStreamHandler("/peercompute/logs/1.0.0", h.handleLogs)
	host.SetStreamHandler("/peercompute/status/1.0.0", h.handleStatus)
	host.SetStreamHandler("/peercompute/stop/1.0.0", h.handleStop)
}

// handleDeploy processes deployment requests.
func (h *Handler) handleDeploy(stream network.Stream) {
	defer stream.Close()

	remotePeer := stream.Conn().RemotePeer()
	log.Printf("[DEPLOY] Request from peer: %s", remotePeer)

	// Read request
	var req protocol.DeployRequest
	if err := readJSON(stream, &req); err != nil {
		log.Printf("[DEPLOY] Failed to read request: %v", err)
		sendError(stream, "invalid request format")
		return
	}

	log.Printf("[DEPLOY] Image: %s, CPU: %d, Memory: %d", req.Image, req.CPUMillicores, req.MemoryBytes)

	// Verify trust
	if !h.trust.IsTrusted(remotePeer) {
		log.Printf("[DEPLOY] Untrusted peer rejected: %s", remotePeer)
		sendError(stream, "not trusted")
		return
	}

	// Pull image
	ctx := context.Background()
	log.Printf("[DEPLOY] Pulling image: %s", req.Image)
	if err := h.runtime.Pull(ctx, req.Image); err != nil {
		log.Printf("[DEPLOY] Image pull failed: %v", err)
		sendError(stream, fmt.Sprintf("failed to pull image: %v", err))
		return
	}

	// Schedule and run via scheduler
	result, err := h.scheduler.Schedule(ctx, &protocol.DeployRequest{
		RequestID:     req.RequestID,
		Image:         req.Image,
		CPUMillicores: req.CPUMillicores,
		MemoryBytes:   req.MemoryBytes,
		ExposePort:    req.ExposePort,
		Environment:   req.Environment,
		RequesterID:   remotePeer.String(),
	})

	if err != nil {
		log.Printf("[DEPLOY] Scheduling failed: %v", err)
		sendError(stream, err.Error())
		return
	}

	log.Printf("[DEPLOY] Success! Deployment: %s, Container: %s", result.ID, result.ContainerID[:12])

	// Register with gateway if tunnel client is available and port is exposed
	var exposedURL string
	if h.tunnelClient != nil && h.tunnelClient.IsConnected() && req.ExposePort > 0 {
		log.Printf("[DEPLOY] Registering with gateway for public access...")
		url, err := h.tunnelClient.RegisterDeployment(result.ID, req.ExposePort)
		if err != nil {
			log.Printf("[DEPLOY] Warning: failed to register with gateway: %v", err)
		} else {
			exposedURL = url
			log.Printf("[DEPLOY] Public URL: %s", exposedURL)
		}
	}

	// Send response
	resp := protocol.DeployResponse{
		Success:      true,
		DeploymentID: result.ID,
		ContainerID:  result.ContainerID,
		ExposedURL:   exposedURL,
		Message:      "Container deployed successfully",
	}
	writeJSON(stream, resp)
}

// handleLogs streams container logs.
func (h *Handler) handleLogs(stream network.Stream) {
	defer stream.Close()

	remotePeer := stream.Conn().RemotePeer()
	log.Printf("[LOGS] Request from peer: %s", remotePeer)

	// Read request
	var req struct {
		DeploymentID string `json:"deployment_id"`
		Follow       bool   `json:"follow"`
		Tail         int    `json:"tail"`
	}
	if err := readJSON(stream, &req); err != nil {
		log.Printf("[LOGS] Failed to read request: %v", err)
		return
	}

	// Get deployment
	deployment, ok := h.scheduler.Get(req.DeploymentID)
	if !ok {
		log.Printf("[LOGS] Deployment not found: %s", req.DeploymentID)
		return
	}

	// Stream logs
	ctx := context.Background()
	logs, err := h.runtime.Logs(ctx, deployment.ContainerID, req.Follow)
	if err != nil {
		log.Printf("[LOGS] Failed to get logs: %v", err)
		return
	}
	defer logs.Close()

	// Copy logs to stream
	io.Copy(stream, logs)
}

// handleStatus returns deployment status.
func (h *Handler) handleStatus(stream network.Stream) {
	defer stream.Close()

	remotePeer := stream.Conn().RemotePeer()
	log.Printf("[STATUS] Request from peer: %s", remotePeer)

	// Read request
	var req protocol.StatusRequest
	if err := readJSON(stream, &req); err != nil {
		log.Printf("[STATUS] Failed to read request: %v", err)
		return
	}

	var resp protocol.StatusResponse

	if req.DeploymentID != "" {
		// Single deployment status
		deployment, ok := h.scheduler.Get(req.DeploymentID)
		if ok {
			resp.Deployments = []protocol.DeploymentStatusInfo{{
				DeploymentID: deployment.ID,
				Status:       string(deployment.Status),
				Image:        deployment.Image,
				StartedAt:    deployment.StartedAt,
			}}
		}
	} else {
		// All deployments
		for _, d := range h.scheduler.List() {
			resp.Deployments = append(resp.Deployments, protocol.DeploymentStatusInfo{
				DeploymentID: d.ID,
				Status:       string(d.Status),
				Image:        d.Image,
				StartedAt:    d.StartedAt,
			})
		}
	}

	writeJSON(stream, resp)
}

// handleStop stops a deployment.
func (h *Handler) handleStop(stream network.Stream) {
	defer stream.Close()

	remotePeer := stream.Conn().RemotePeer()
	log.Printf("[STOP] Request from peer: %s", remotePeer)

	// Read request
	var req protocol.StopRequest
	if err := readJSON(stream, &req); err != nil {
		log.Printf("[STOP] Failed to read request: %v", err)
		sendError(stream, "invalid request format")
		return
	}

	// Unregister from gateway if tunnel client is available
	if h.tunnelClient != nil && h.tunnelClient.IsConnected() {
		h.tunnelClient.UnregisterDeployment(req.DeploymentID)
	}

	// Stop via scheduler
	ctx := context.Background()
	if err := h.scheduler.Stop(ctx, req.DeploymentID); err != nil {
		log.Printf("[STOP] Failed to stop: %v", err)
		sendError(stream, fmt.Sprintf("failed to stop: %v", err))
		return
	}

	log.Printf("[STOP] Stopped deployment: %s", req.DeploymentID)

	resp := protocol.StopResponse{
		Success:      true,
		DeploymentID: req.DeploymentID,
		Message:      "Container stopped",
	}
	writeJSON(stream, resp)
}

// Helper functions

func readJSON(r io.Reader, v interface{}) error {
	decoder := json.NewDecoder(r)
	return decoder.Decode(v)
}

func writeJSON(w io.Writer, v interface{}) error {
	encoder := json.NewEncoder(w)
	return encoder.Encode(v)
}

func sendError(w io.Writer, message string) {
	resp := protocol.DeployResponse{
		Success: false,
		Message: message,
	}
	writeJSON(w, resp)
}
