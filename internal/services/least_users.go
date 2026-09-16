package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/masteralanlab/free-proxy/internal/config"
	"github.com/masteralanlab/free-proxy/internal/domain"
	"github.com/masteralanlab/free-proxy/internal/store"
)

// exitStatusProvider reports gateway state.
type exitStatusProvider interface {
	Status() domain.GatewayStatus
}

// exitActivator brings a node up as the active exit.
type exitActivator interface {
	Activate(ctx context.Context, nodeID string) (domain.TunnelStartResult, error)
}

// exitController is what the monitor needs from the gateway; *GatewayService
// satisfies it, and tests substitute a fake.
type exitController interface {
	exitStatusProvider
	exitActivator
}

// LeastUsersMonitor keeps the exit on the least-used node of the policy's IP
// class. The three least-users routing modes promise that: pick the ready node
// with the lowest user count, take over immediately when the current exit dies
// (via the shared auto-switch paths), and re-check periodically so a node that
// later becomes busier than a peer does not keep the exit just because it got
// there first.
type LeastUsersMonitor struct {
	cfg          *config.Config
	nodes        *store.NodeRepository
	settingsRepo *store.SettingsRepository
	gateway      exitStatusProvider
	activator    exitActivator
	State        MonitorState
}

// NewLeastUsersMonitor constructs a LeastUsersMonitor.
func NewLeastUsersMonitor(cfg *config.Config, nodes *store.NodeRepository, settingsRepo *store.SettingsRepository, gateway exitController) *LeastUsersMonitor {
	return &LeastUsersMonitor{cfg: cfg, nodes: nodes, settingsRepo: settingsRepo, gateway: gateway, activator: gateway}
}

// Run loops until ctx is cancelled.
func (m *LeastUsersMonitor) Run(ctx context.Context) {
	t := time.NewTicker(m.cfg.LeastUsersCheckInterval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := m.Rebalance(ctx); err != nil {
				m.State.Heartbeat(false, err.Error())
				slog.Warn("least-users rebalance failed", "module", "least_users", "err", err)
			} else {
				m.State.Heartbeat(true, "")
			}
		}
	}
}

// Rebalance activates the least-used ready node of the selected IP class when
// it differs from the active exit. Activation failures mark the candidate
// unavailable, so each pass walks down the ranking instead of retrying the same
// broken node forever.
func (m *LeastUsersMonitor) Rebalance(ctx context.Context) error {
	settings, err := m.settingsRepo.Get(ctx)
	if err != nil {
		return err
	}
	if !settings.ConnectionEnabled {
		return nil
	}
	if _, ok := LeastUsersIPType(settings.RoutingMode); !ok {
		return nil
	}
	active := m.gateway.Status().ActiveNodeID
	if active == nil {
		return nil
	}

	excluded := map[string]bool{}
	for i := 0; i < 3; i++ {
		candidates, err := m.nodes.ListNodes(ctx, store.NodeFilter{Status: string(domain.NodeReady)}, candidateFetchLimit, 0)
		if err != nil {
			return err
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
			return nil
		}
		best := filtered[0]
		if best.ID == *active {
			return nil // already on the least-used node of this IP class
		}
		slog.Info("switching to least-used node", "module", "least_users",
			"from", *active, "to", best.ID, "sessions", best.SourceSessions,
			"ip_type", settings.RoutingMode)
		res, err := m.activator.Activate(ctx, best.ID)
		if err == nil && res.Success {
			return nil
		}
		if err != nil {
			// Local fault (database, device, routing) — not the node's fault.
			excluded[best.ID] = true
			continue
		}
		_ = m.nodes.Blacklist(ctx, best.ID, rebalanceFailureMessage(res.Message), m.cfg.InvalidBackoff())
		excluded[best.ID] = true
	}
	return nil
}

func rebalanceFailureMessage(fallback string) string {
	if fallback != "" {
		return fallback
	}
	return "least-users rebalance activation failed"
}
