package services

import (
	"context"
	"time"

	"github.com/masteralanlab/free-proxy/internal/config"
	"github.com/masteralanlab/free-proxy/internal/netx"
	"github.com/masteralanlab/free-proxy/internal/store"
)

// ActiveLatencyMonitor periodically refreshes the active node's latency.
type ActiveLatencyMonitor struct {
	nodes   *store.NodeRepository
	gateway *GatewayService
	runner  netx.CommandRunner
	State   MonitorState
}

// NewActiveLatencyMonitor constructs an ActiveLatencyMonitor.
func NewActiveLatencyMonitor(nodes *store.NodeRepository, gateway *GatewayService, runner netx.CommandRunner) *ActiveLatencyMonitor {
	return &ActiveLatencyMonitor{nodes: nodes, gateway: gateway, runner: runner}
}

// Run loops until ctx is cancelled.
func (m *ActiveLatencyMonitor) Run(ctx context.Context) {
	t := time.NewTicker(config.ActivePingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if active := m.gateway.Status().ActiveNodeID; active != nil {
				if target, err := m.nodes.GetTarget(ctx, *active); err == nil {
					latency := netx.MeasureNodeLatency(ctx, m.runner, target.RemoteHost, target.RemotePort, target.SourcePingMS)
					m.gateway.UpdateActiveLatency(latency)
				}
			}
			m.State.Heartbeat(true, "")
		}
	}
}
