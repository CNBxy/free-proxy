package services

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/masteralanlab/free-proxy/internal/config"
	"github.com/masteralanlab/free-proxy/internal/domain"
	"github.com/masteralanlab/free-proxy/internal/netx"
	"github.com/masteralanlab/free-proxy/internal/store"
	"github.com/masteralanlab/free-proxy/internal/tunnel"
)

// A TCP endpoint that refuses or drops a connect cannot carry an
// OpenVPN-over-TCP handshake, so the probe path asks that question first. The
// handshake itself costs a full OpenVPN process and up to the whole test
// timeout per node; the dial costs milliseconds when answered and at most
// tcpPrecheckTimeout when not. On a pool where most nodes are dead this turns
// a cycle that ran tens of minutes into one that runs in seconds — the same
// trade the liveness sweep already makes pool-wide (see liveness.go), applied
// to the one batch path its filter cannot cover: freshly discovered nodes.
const (
	tcpPrecheckConcurrency = 32
	tcpPrecheckTimeout     = 3 * time.Second
)

// ProbeService dials nodes to test connectivity and measure latency.
type ProbeService struct {
	cfg         *config.Config
	nodes       *store.NodeRepository
	tunnel      *tunnel.Manager
	tunAlloc    *netx.TunAllocator
	runner      netx.CommandRunner
	ipInfo      *IpInfoService
	history     *store.ProbeResultRepository
	coordinator *Coordinator
	sem         chan struct{}
	presem      chan struct{}
	// dial backs the TCP pre-check; swapped out in tests.
	dial func(ctx context.Context, addr string, timeout time.Duration) bool
}

// NewProbeService constructs a ProbeService.
func NewProbeService(cfg *config.Config, nodes *store.NodeRepository, mgr *tunnel.Manager, tunAlloc *netx.TunAllocator,
	runner netx.CommandRunner, ipInfo *IpInfoService, history *store.ProbeResultRepository, coordinator *Coordinator) *ProbeService {
	n := cfg.MaxProbeConcurrency
	if n < 1 {
		n = 1
	}
	return &ProbeService{
		cfg: cfg, nodes: nodes, tunnel: mgr, tunAlloc: tunAlloc, runner: runner,
		ipInfo: ipInfo, history: history, coordinator: coordinator, sem: make(chan struct{}, n),
		presem: make(chan struct{}, tcpPrecheckConcurrency),
		dial:   dialTCP,
	}
}

// Probe tests a single node, updating its state and (optionally) enriching IP info.
func (s *ProbeService) Probe(ctx context.Context, nodeID string, enrich bool) (domain.ProbeResult, error) {
	target, err := s.nodes.GetTarget(ctx, nodeID)
	if err != nil {
		return domain.ProbeResult{}, err
	}
	_ = s.nodes.MarkProbing(ctx, nodeID)

	if s.unreachableOverTCP(ctx, target) {
		return s.recordUnreachable(ctx, nodeID), nil
	}

	var latency int
	var tun domain.TunnelStartResult
	func() {
		s.sem <- struct{}{}
		defer func() { <-s.sem }()
		device, release, allocErr := s.tunAlloc.Allocate()
		if allocErr != nil {
			tun = failureResult(allocErr)
			return
		}
		defer release()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			latency = netx.MeasureNodeLatency(ctx, s.runner, target.RemoteHost, target.RemotePort, target.SourcePingMS)
		}()
		go func() {
			defer wg.Done()
			tun = s.tunnel.Probe(ctx, target.ConfigText, device)
		}()
		wg.Wait()
	}()

	probedAt := time.Now().UTC()
	result := domain.ProbeResult{
		NodeID: nodeID, Available: tun.Success, LatencyMS: latency, Tunnel: tun, ProbedAt: probedAt,
	}
	s.recordResult(ctx, nodeID, result)
	if enrich && result.Available && s.ipInfo != nil {
		_ = s.ipInfo.Enrich(ctx, nodeID, target.IPAddress)
	}
	return result, nil
}

// unreachableOverTCP reports whether a TCP-transport node refuses connections.
// UDP endpoints have no cheap verdict (see ListTCPLivenessTargets), so they skip
// straight to the handshake; malformed targets do too, letting OpenVPN produce
// the authoritative failure for whatever the config really contains.
func (s *ProbeService) unreachableOverTCP(ctx context.Context, target domain.ProxyNodeTarget) bool {
	if target.Transport != domain.TransportTCP || target.RemoteHost == "" || target.RemotePort <= 0 {
		return false
	}
	s.presem <- struct{}{}
	defer func() { <-s.presem }()
	addr := net.JoinHostPort(target.RemoteHost, strconv.Itoa(target.RemotePort))
	return !s.dial(ctx, addr, tcpPrecheckTimeout)
}

// recordUnreachable closes a pre-check failure with the same bookkeeping a
// handshake failure would get, so counters and history stay comparable.
func (s *ProbeService) recordUnreachable(ctx context.Context, nodeID string) domain.ProbeResult {
	code := domain.FailUnreachable
	result := domain.ProbeResult{
		NodeID:    nodeID,
		Available: false,
		Tunnel: domain.TunnelStartResult{
			Status:         domain.TunnelFailed,
			Message:        "Node did not answer a TCP connect; skipped the OpenVPN handshake",
			FailureCode:    &code,
			HandshakeStage: "starting",
		},
		ProbedAt: time.Now().UTC(),
	}
	s.recordResult(ctx, nodeID, result)
	return result
}

func (s *ProbeService) recordResult(ctx context.Context, nodeID string, result domain.ProbeResult) {
	_ = s.nodes.UpdateProbeResult(ctx, nodeID, result.Available, result.LatencyMS, result.ProbedAt)
	if s.history != nil {
		_, _ = s.history.Insert(ctx, result)
	}
}

// ProbeMany tests several nodes concurrently (bounded by the semaphore).
func (s *ProbeService) ProbeMany(ctx context.Context, nodeIDs []string) ([]domain.ProbeResult, error) {
	var results []domain.ProbeResult
	err := s.coordinator.Run(ctx, "probe", false, func(ctx context.Context) error {
		results = s.probeMany(ctx, nodeIDs)
		return nil
	})
	return results, err
}

func (s *ProbeService) probeMany(ctx context.Context, nodeIDs []string) []domain.ProbeResult {
	seen := map[string]bool{}
	var unique []string
	for _, id := range nodeIDs {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	results := make([]domain.ProbeResult, len(unique))
	var wg sync.WaitGroup
	for i, id := range unique {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			res, err := s.Probe(ctx, id, false)
			if err != nil {
				res = domain.ProbeResult{NodeID: id, Available: false, Tunnel: failureResult(err), ProbedAt: time.Now().UTC()}
				_ = s.nodes.UpdateProbeResult(ctx, id, false, 0, res.ProbedAt)
			}
			results[i] = res
		}(i, id)
	}
	wg.Wait()

	if s.ipInfo != nil {
		successful := map[string]string{}
		for _, r := range results {
			if !r.Available {
				continue
			}
			if target, err := s.nodes.GetTarget(ctx, r.NodeID); err == nil {
				successful[r.NodeID] = target.IPAddress
			}
		}
		if len(successful) > 0 {
			_ = s.ipInfo.EnrichMany(ctx, successful)
		}
	}
	return results
}

// ProbeJob is the JobFunc form of Probe.
func (s *ProbeService) ProbeJob(nodeID string) JobFunc {
	return func(ctx context.Context) (map[string]any, error) {
		res, err := s.Probe(ctx, nodeID, true)
		if err != nil {
			return nil, err
		}
		return toMap(res)
	}
}

// ProbeManyJob is the JobFunc form of ProbeMany.
func (s *ProbeService) ProbeManyJob(nodeIDs []string) JobFunc {
	return func(ctx context.Context) (map[string]any, error) {
		res, err := s.ProbeMany(ctx, nodeIDs)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(res))
		for _, r := range res {
			m, err := toMap(r)
			if err != nil {
				return nil, err
			}
			items = append(items, m)
		}
		return map[string]any{"nodes": items}, nil
	}
}

func failureResult(err error) domain.TunnelStartResult {
	code := domain.FailUnknown
	return domain.TunnelStartResult{
		Success:        false,
		Status:         domain.TunnelFailed,
		Message:        fmt.Sprintf("Probe failed: %v", err),
		FailureCode:    &code,
		HandshakeStage: "starting",
	}
}
