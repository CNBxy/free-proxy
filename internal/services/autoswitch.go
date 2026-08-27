package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/masteralanlab/free-proxy/internal/config"
	"github.com/masteralanlab/free-proxy/internal/domain"
	"github.com/masteralanlab/free-proxy/internal/store"
)

const (
	// minStableUptime separates "this node works" from "this node completes a
	// handshake and then drops". A tunnel that dies inside this window never
	// carried real traffic, and nothing else in the system will retire the node
	// behind it: it passed its probe and it passed its handshake, so the next
	// selection ranks it first and picks it straight back up.
	minStableUptime = 90 * time.Second

	// reconnectStreakWindow is how long consecutive unexpected exits count as one
	// episode. A tunnel that survives it resets the streak.
	reconnectStreakWindow = 10 * time.Minute

	// maxReconnectBackoff caps the wait. The health monitor runs its own recovery
	// every HEALTH_CHECK_INTERVAL_SECONDS regardless, so a long backoff delays the
	// reconnect rather than abandoning it.
	maxReconnectBackoff = 5 * time.Minute
)

// AutoSwitchService picks a replacement exit after a failure, blacklisting nodes
// that fail to activate.
type AutoSwitchService struct {
	nodes        *store.NodeRepository
	settingsRepo *store.SettingsRepository
	pool         *ProxyPoolService
	gateway      *GatewayService

	mu         sync.Mutex
	lastExitAt time.Time
	exitStreak int
}

// NewAutoSwitchService constructs an AutoSwitchService.
func NewAutoSwitchService(nodes *store.NodeRepository, settingsRepo *store.SettingsRepository, pool *ProxyPoolService, gateway *GatewayService) *AutoSwitchService {
	return &AutoSwitchService{nodes: nodes, settingsRepo: settingsRepo, pool: pool, gateway: gateway}
}

// Switch activates the best alternate node, or the fixed node in fixed mode.
func (s *AutoSwitchService) Switch(ctx context.Context) (*domain.TunnelStartResult, error) {
	return s.switchExcluding(ctx, nil)
}

// switchExcluding is Switch with a caller-supplied set of nodes to skip, on top
// of the currently active one.
func (s *AutoSwitchService) switchExcluding(ctx context.Context, skip map[string]bool) (*domain.TunnelStartResult, error) {
	settings, err := s.settingsRepo.Get(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.ConnectionEnabled {
		return nil, nil
	}
	if settings.RoutingMode == domain.PolicyFixed {
		if settings.FixedNodeID == nil || *settings.FixedNodeID == "" {
			return nil, nil
		}
		res, err := s.gateway.Activate(ctx, *settings.FixedNodeID)
		return &res, err
	}

	excluded := map[string]bool{}
	for id := range skip {
		excluded[id] = true
	}
	if active := s.gateway.Status().ActiveNodeID; active != nil {
		excluded[*active] = true
	}
	for i := 0; i < 3; i++ {
		candidate, err := s.selectExcluding(ctx, excluded)
		if err != nil {
			return nil, err
		}
		if candidate == nil {
			s.gateway.DisconnectOnly(ctx)
			return nil, nil
		}
		res, err := s.gateway.Activate(ctx, candidate.ID)
		if err == nil && res.Success {
			return &res, nil
		}
		// A conflict is the coordinator refusing every activation for as long as
		// another operation holds it, so the next candidate hits the same wall
		// within milliseconds. Retrying spent the whole budget on one lock —
		// three activations, three log lines, no exit — and reported nothing to
		// the caller. Hand the conflict back and let it be retried later.
		if errors.Is(err, domain.ErrOperationConflict) {
			return nil, err
		}
		excluded[candidate.ID] = true
		// Activate already separates the two failures: a non-nil error is ours
		// (database, device, routing, a candidate the policy should not have
		// offered), while !res.Success is the node refusing the handshake. Only
		// the second is the node's fault. Charging the first to it put healthy
		// nodes into cooldown for a local fault — three per rotation, every
		// rotation, for as long as the fault lasted.
		if err != nil {
			slog.Warn("activation failed locally; trying another node without penalising this one",
				"module", "autoswitch", "node", candidate.ID, "err", err)
			continue
		}
		msg := "activation failed"
		if res.Message != "" {
			msg = res.Message
		}
		_ = s.nodes.Blacklist(ctx, candidate.ID, msg, config.InvalidBackoff)
	}
	return nil, nil
}

// HandleUnexpectedExit reconnects after an unexpected tunnel exit.
//
// Two things keep this off the spin it used to be. A node whose tunnel died
// inside minStableUptime goes into cooldown, because otherwise the very next
// selection hands back the node that just failed and the cycle repeats at
// whatever rate OpenVPN can be forked. And consecutive exits back off, so a
// fault no node can escape — a dead uplink, policy routes that will not install
// — costs one attempt every few minutes instead of a continuous loop of process
// launches, `ip rule` invocations, and log writes.
func (s *AutoSwitchService) HandleUnexpectedExit(ctx context.Context, nodeID string, uptime time.Duration) {
	settings, err := s.settingsRepo.Get(ctx)
	if err != nil || !settings.ConnectionEnabled {
		return
	}

	if nodeID != "" && uptime > 0 && uptime < minStableUptime {
		msg := fmt.Sprintf("tunnel dropped %s after connecting", uptime.Round(time.Second))
		_ = s.nodes.Blacklist(ctx, nodeID, msg, config.InvalidBackoff)
		slog.Warn("exit node dropped its tunnel too quickly; entering cooldown",
			"module", "autoswitch", "node", nodeID, "uptime", uptime.Round(time.Second))
	}

	if delay := s.noteUnexpectedExit(); delay > 0 {
		slog.Warn("backing off before reconnecting after repeated tunnel exits",
			"module", "autoswitch", "delay", delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}

	// No fixed-mode branch here on purpose: switchExcluding handles fixed mode
	// itself, and it re-reads the settings. Deciding from the copy read above
	// would be deciding from a snapshot up to maxReconnectBackoff old — long
	// enough for an operator to have disabled the connection or repointed the
	// fixed node, and this path would have reconnected anyway.
	// Naming the failed node matters even when it was not put into cooldown: the
	// gateway clears its active id before calling us, so the selection below has
	// no other way to know which node it just lost.
	skip := map[string]bool{}
	if nodeID != "" {
		skip[nodeID] = true
	}
	_, _ = s.switchExcluding(ctx, skip)
}

// noteUnexpectedExit records an exit and returns how long to wait before
// reconnecting.
func (s *AutoSwitchService) noteUnexpectedExit() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.lastExitAt.IsZero() || now.Sub(s.lastExitAt) > reconnectStreakWindow {
		s.exitStreak = 0
	}
	s.exitStreak++
	s.lastExitAt = now
	return reconnectBackoff(s.exitStreak)
}

// reconnectBackoff returns the wait before the nth consecutive reconnect. The
// first is immediate: one unexpected exit is ordinary, and recovering from it
// fast is the whole point of the gateway. What follows grows, because a second
// and third exit in quick succession mean the reconnecting is itself the
// problem.
func reconnectBackoff(streak int) time.Duration {
	if streak <= 1 {
		return 0
	}
	d := 5 * time.Second
	for i := 2; i < streak; i++ {
		d *= 2
		if d >= maxReconnectBackoff {
			return maxReconnectBackoff
		}
	}
	return d
}

func (s *AutoSwitchService) selectExcluding(ctx context.Context, excluded map[string]bool) (*domain.ProxyNodeRead, error) {
	candidates, err := s.nodes.ListNodes(ctx, store.NodeFilter{Status: string(domain.NodeReady)}, 1000, 0)
	if err != nil {
		return nil, err
	}
	settings, err := s.settingsRepo.Get(ctx)
	if err != nil {
		return nil, err
	}
	candidates = ApplyFilters(candidates, settings, false)
	filtered := candidates[:0]
	for _, n := range candidates {
		if !excluded[n.ID] {
			filtered = append(filtered, n)
		}
	}
	SortCandidates(filtered, settings)
	if len(filtered) == 0 {
		return nil, nil
	}
	best := filtered[0]
	return &best, nil
}
