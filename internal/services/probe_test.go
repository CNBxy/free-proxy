package services

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/masteralanlab/free-proxy/internal/config"
	"github.com/masteralanlab/free-proxy/internal/domain"
	"github.com/masteralanlab/free-proxy/internal/netx"
	"github.com/masteralanlab/free-proxy/internal/store"
	"github.com/masteralanlab/free-proxy/internal/tunnel"
)

// newTestProber wires a ProbeService whose OpenVPN binary never exists, so any
// probe that reaches the handshake stage fails fast with FailCommandNotFound.
// The dial verdict comes from reachable[addr], exactly like the sweep tests.
func newTestProber(t *testing.T, repos *store.Repos, reachable map[string]bool) *ProbeService {
	t.Helper()
	cfg := &config.Config{DataDir: t.TempDir(), OpenVPNCommand: "free-proxy-test-missing-openvpn"}
	tunAlloc, err := netx.NewTunAllocator("", 1, 4)
	if err != nil {
		t.Fatalf("tun allocator: %v", err)
	}
	svc := NewProbeService(cfg, repos.Nodes, tunnel.NewManager(cfg), tunAlloc,
		netx.SystemCommandRunner{}, nil, repos.Probes, NewCoordinator())
	svc.dial = func(_ context.Context, addr string, _ time.Duration) bool { return reachable[addr] }
	return svc
}

// localDisc builds a node pointing at a real loopback listener, so any latency
// measurement that runs alongside the handshake returns instantly instead of
// waiting out its TCP timeout against a blackhole address.
func localDisc(t *testing.T, id string, transport domain.TransportProtocol) domain.DiscoveredNode {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	port := ln.Addr().(*net.TCPAddr).Port
	n := disc(id, "127.0.0.1")
	n.Transport = transport
	n.RemotePort = port
	return n
}

func probeFailureCode(t *testing.T, res domain.ProbeResult) domain.TunnelFailureCode {
	t.Helper()
	if res.Available || res.Tunnel.FailureCode == nil {
		t.Fatalf("probe result = %+v, want a failure code", res)
	}
	return *res.Tunnel.FailureCode
}

func TestProbeSkipsHandshakeWhenTCPPrecheckFails(t *testing.T) {
	repos := newTestRepos(t)
	seed(t, repos, 0, discTCP("jp-1", "198.51.100.7"))
	svc := newTestProber(t, repos, map[string]bool{})

	res, err := svc.Probe(context.Background(), "jp-1", false)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	// A host that refuses TCP cannot carry OpenVPN-over-TCP, so the expensive
	// handshake must be skipped entirely (its failure would read command-not-found).
	if got := probeFailureCode(t, res); got != domain.FailUnreachable {
		t.Fatalf("failure code = %q, want unreachable from the pre-check", got)
	}
	var status string
	if err := repos.DB.QueryRow("SELECT status FROM proxy_nodes WHERE id = 'jp-1'").Scan(&status); err != nil {
		t.Fatalf("status: %v", err)
	}
	if status != string(domain.NodeUnavailable) {
		t.Fatalf("status = %q, want unavailable after a refused connect", status)
	}
	history, err := repos.Probes.ListForNode(context.Background(), "jp-1", 10)
	if err != nil || len(history) != 1 {
		t.Fatalf("history rows = %d err %v, want the pre-check verdict recorded once", len(history), err)
	}
}

func TestProbeRunsHandshakeWhenTCPPrecheckPasses(t *testing.T) {
	repos := newTestRepos(t)
	n := localDisc(t, "jp-1", domain.TransportTCP)
	seed(t, repos, 0, n)
	reachable := map[string]bool{net.JoinHostPort(n.RemoteHost, strconv.Itoa(n.RemotePort)): true}
	svc := newTestProber(t, repos, reachable)

	res, err := svc.Probe(context.Background(), "jp-1", false)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	// A reachable endpoint must go on to the handshake stage, which here fails
	// with the missing test binary rather than the pre-check's verdict.
	if got := probeFailureCode(t, res); got != domain.FailCommandNotFound {
		t.Fatalf("failure code = %q, want the handshake to have been attempted", got)
	}
}

func TestProbeSkipsPrecheckForUDPNodes(t *testing.T) {
	repos := newTestRepos(t)
	n := localDisc(t, "udp-1", domain.TransportUDP)
	seed(t, repos, 0, n)
	// Nothing is reachable per the injected dial, yet the UDP node must reach
	// the handshake anyway: a refused TCP connect says nothing about UDP.
	svc := newTestProber(t, repos, map[string]bool{})

	res, err := svc.Probe(context.Background(), "udp-1", false)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got := probeFailureCode(t, res); got != domain.FailCommandNotFound {
		t.Fatalf("failure code = %q, want UDP nodes exempt from the TCP pre-check", got)
	}
}

func TestProbeManyRecordsPrecheckFailures(t *testing.T) {
	repos := newTestRepos(t)
	seed(t, repos, 0, discTCP("jp-1", "198.51.100.7"), discTCP("us-1", "203.0.113.9"))
	svc := newTestProber(t, repos, map[string]bool{})

	results := svc.probeMany(context.Background(), []string{"jp-1", "us-1"})
	if len(results) != 2 {
		t.Fatalf("results = %d, want one per node", len(results))
	}
	for _, r := range results {
		if r.Available {
			t.Fatalf("%s reported available; nothing was reachable", r.NodeID)
		}
	}
	var unavailable int
	if err := repos.DB.QueryRow(
		"SELECT COUNT(*) FROM proxy_nodes WHERE status = 'unavailable'").Scan(&unavailable); err != nil {
		t.Fatalf("count unavailable: %v", err)
	}
	if unavailable != 2 {
		t.Fatalf("unavailable rows = %d, want both batch members demoted", unavailable)
	}
}
