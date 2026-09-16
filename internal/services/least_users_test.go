package services

import (
	"context"
	"testing"

	"github.com/masteralanlab/free-proxy/internal/config"
	"github.com/masteralanlab/free-proxy/internal/domain"
	"github.com/masteralanlab/free-proxy/internal/store"
)

type fakeGateway struct {
	active    string
	failIDs   map[string]bool
	activated []string
}

func (f *fakeGateway) Status() domain.GatewayStatus {
	s := domain.GatewayStatus{}
	if f.active != "" {
		id := f.active
		s.ActiveNodeID = &id
	}
	return s
}

func (f *fakeGateway) Activate(_ context.Context, nodeID string) (domain.TunnelStartResult, error) {
	if f.failIDs[nodeID] {
		return domain.TunnelStartResult{Success: false, Message: "handshake refused"}, nil
	}
	f.active = nodeID
	f.activated = append(f.activated, nodeID)
	return domain.TunnelStartResult{Success: true, Status: domain.TunnelConnected}, nil
}

func newLeastUsersFixture(t *testing.T, mode domain.ProxyPolicyMode) (*LeastUsersMonitor, *fakeGateway, *store.Repos) {
	t.Helper()
	repos := newTestRepos(t)
	ctx := context.Background()
	settings := domain.ProxySettingsUpdate{
		RoutingMode: mode, RoutingIPType: domain.RoutingAll, ConnectionEnabled: true,
	}
	if err := repos.Settings.Update(ctx, settings); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	gw := &fakeGateway{failIDs: map[string]bool{}}
	cfg := &config.Config{}
	return NewLeastUsersMonitor(cfg, repos.Nodes, repos.Settings, gw), gw, repos
}

// seedLeastUsers stores four ready nodes of different IP classes with distinct
// session counts (the user-count signal).
func seedLeastUsers(t *testing.T, repos *store.Repos) {
	t.Helper()
	nodes := []domain.DiscoveredNode{
		disc("res-busy", "10.0.0.1"), disc("res-quiet", "10.0.0.2"),
		disc("mob", "10.0.0.3"), disc("host", "10.0.0.4"),
	}
	for i := range nodes {
		nodes[i].Transport = domain.TransportTCP
	}
	if _, err := repos.Nodes.UpsertDiscovered(context.Background(), nodes); err != nil {
		t.Fatalf("seed: %v", err)
	}
	sessions := map[string]int{"res-busy": 40, "res-quiet": 2, "mob": 7, "host": 99}
	for id, sessionsN := range sessions {
		if _, err := repos.DB.Exec("UPDATE proxy_nodes SET source_sessions = ?, status = 'ready' WHERE id = ?",
			sessionsN, id); err != nil {
			t.Fatalf("update %s: %v", id, err)
		}
	}
}

func TestLeastUsersIPTypeMapping(t *testing.T) {
	cases := map[domain.ProxyPolicyMode]domain.IpType{
		domain.PolicyResidentialLeastUsers: domain.IpResidential,
		domain.PolicyMobileLeastUsers:      domain.IpMobile,
		domain.PolicyHostingLeastUsers:     domain.IpHosting,
	}
	for mode, want := range cases {
		got, ok := LeastUsersIPType(mode)
		if !ok || got != want {
			t.Fatalf("LeastUsersIPType(%s) = (%s, %v), want (%s, true)", mode, got, ok, want)
		}
		if !IsLeastUsersMode(mode) {
			t.Fatalf("IsLeastUsersMode(%s) = false, want true", mode)
		}
	}
	for _, mode := range []domain.ProxyPolicyMode{domain.PolicyAuto, domain.PolicyFixed, domain.PolicySmart} {
		if IsLeastUsersMode(mode) {
			t.Fatalf("IsLeastUsersMode(%s) = true, want false", mode)
		}
	}
}

func TestApplyFiltersLeastUsersModes(t *testing.T) {
	nodes := []domain.ProxyNodeRead{
		node("res", 10, 1, domain.IpResidential, "JP"),
		node("mob", 10, 1, domain.IpMobile, "JP"),
		node("host", 10, 1, domain.IpHosting, "JP"),
		node("unk", 10, 1, domain.IpUnknown, "JP"),
	}
	cases := []struct {
		mode domain.ProxyPolicyMode
		want string
	}{
		{domain.PolicyResidentialLeastUsers, "res"},
		{domain.PolicyMobileLeastUsers, "mob"},
		{domain.PolicyHostingLeastUsers, "host"},
	}
	for _, c := range cases {
		settings := domain.ProxySettings{RoutingMode: c.mode, RoutingIPType: domain.RoutingAll}
		got := ApplyFilters(nodes, settings, false)
		if len(got) != 1 || got[0].ID != c.want {
			t.Fatalf("%s filter = %v, want only %s", c.mode, ids(got), c.want)
		}
		// The implied IP class wins over a conflicting RoutingIPType.
		for _, ipFilter := range []domain.RoutingIpType{domain.RoutingResidential, domain.RoutingHosting} {
			settings.RoutingIPType = ipFilter
			got = ApplyFilters(nodes, settings, false)
			if len(got) != 1 || got[0].ID != c.want {
				t.Fatalf("%s filter with RoutingIPType=%s = %v, want only %s", c.mode, ipFilter, ids(got), c.want)
			}
		}
	}
}

func TestSortCandidatesLeastUsers(t *testing.T) {
	nodes := []domain.ProxyNodeRead{
		{ID: "busy", LatencyMS: 5, SourceSessions: 42},
		{ID: "quiet", LatencyMS: 90, SourceSessions: 3},
		{ID: "mid", LatencyMS: 50, SourceSessions: 10},
	}
	SortCandidates(nodes, domain.ProxySettings{RoutingMode: domain.PolicyResidentialLeastUsers})
	if nodes[0].ID != "quiet" || nodes[1].ID != "mid" || nodes[2].ID != "busy" {
		t.Fatalf("least-users should rank by ascending sessions, got %v", ids(nodes))
	}

	tied := []domain.ProxyNodeRead{
		{ID: "slow", LatencyMS: 80, SourceSessions: 7},
		{ID: "fast", LatencyMS: 12, SourceSessions: 7},
	}
	SortCandidates(tied, domain.ProxySettings{RoutingMode: domain.PolicyMobileLeastUsers})
	if tied[0].ID != "fast" {
		t.Fatalf("session tie should fall back to latency, got %s first", tied[0].ID)
	}

	fresh := []domain.ProxyNodeRead{
		{ID: "slow", LatencyMS: 120},
		{ID: "fast", LatencyMS: 9},
	}
	SortCandidates(fresh, domain.ProxySettings{RoutingMode: domain.PolicyHostingLeastUsers})
	if fresh[0].ID != "fast" {
		t.Fatalf("zero-session tie should fall back to latency, got %s first", fresh[0].ID)
	}
}

func TestRebalanceSwitchesToLeastUsed(t *testing.T) {
	m, gw, repos := newLeastUsersFixture(t, domain.PolicyResidentialLeastUsers)
	seedLeastUsers(t, repos)
	gw.active = "res-busy"

	if err := m.Rebalance(context.Background()); err != nil {
		t.Fatalf("Rebalance: %v", err)
	}
	if gw.active != "res-quiet" {
		t.Fatalf("active = %q, want res-quiet (2 sessions beats 40)", gw.active)
	}
}

func TestRebalanceKeepsOptimalExit(t *testing.T) {
	m, gw, repos := newLeastUsersFixture(t, domain.PolicyResidentialLeastUsers)
	seedLeastUsers(t, repos)
	gw.active = "res-quiet"

	if err := m.Rebalance(context.Background()); err != nil {
		t.Fatalf("Rebalance: %v", err)
	}
	if gw.active != "res-quiet" || len(gw.activated) != 0 {
		t.Fatalf("optimal exit must not be re-activated, active = %q activated = %v", gw.active, gw.activated)
	}
}

func TestRebalanceSkipsOtherModes(t *testing.T) {
	m, gw, repos := newLeastUsersFixture(t, domain.PolicyAuto)
	seedLeastUsers(t, repos)
	gw.active = "res-busy"

	if err := m.Rebalance(context.Background()); err != nil {
		t.Fatalf("Rebalance: %v", err)
	}
	if gw.active != "res-busy" || len(gw.activated) != 0 {
		t.Fatalf("auto mode must not rebalance, active = %q activated = %v", gw.active, gw.activated)
	}
}

func TestRebalanceBlacklistsFailingCandidateAndMovesOn(t *testing.T) {
	m, gw, repos := newLeastUsersFixture(t, domain.PolicyMobileLeastUsers)
	seedOne := func(id, ip, ipType string, sessions int) {
		n := disc(id, ip)
		n.Transport = domain.TransportTCP
		if _, err := repos.Nodes.UpsertDiscovered(context.Background(), []domain.DiscoveredNode{n}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
		if _, err := repos.DB.Exec("UPDATE proxy_nodes SET source_sessions=?,status=?,ip_type=? WHERE id=?",
			sessions, "ready", ipType, id); err != nil {
			t.Fatalf("update %s: %v", id, err)
		}
	}
	seedOne("mob-busy", "10.1.0.1", string(domain.IpMobile), 50)
	seedOne("mob-quiet", "10.1.0.2", string(domain.IpMobile), 1)
	gw.active = "mob-busy"
	gw.failIDs["mob-quiet"] = true

	if err := m.Rebalance(context.Background()); err != nil {
		t.Fatalf("Rebalance: %v", err)
	}
	if gw.active != "mob-busy" {
		t.Fatalf("all candidates failed; active should stay mob-busy, got %q", gw.active)
	}
	var n int
	if err := repos.DB.QueryRow("SELECT COUNT(*) FROM node_blacklist WHERE node_id = 'mob-quiet'").Scan(&n); err != nil {
		t.Fatalf("read blacklist: %v", err)
	}
	if n != 1 {
		t.Fatal("failed candidate was not blacklisted")
	}

	// Once the failing node recovers it is no longer blacklisted and the next
	// pass completes the switch.
	gw.failIDs["mob-quiet"] = false
	if _, err := repos.DB.Exec("DELETE FROM node_blacklist WHERE node_id = 'mob-quiet'"); err != nil {
		t.Fatalf("clear blacklist: %v", err)
	}
	if err := m.Rebalance(context.Background()); err != nil {
		t.Fatalf("second Rebalance: %v", err)
	}
	if gw.active != "mob-quiet" {
		t.Fatalf("active = %q, want mob-quiet after recovery", gw.active)
	}
}

func mutate(n domain.ProxyNodeRead, f func(*domain.ProxyNodeRead)) domain.ProxyNodeRead {
	f(&n)
	return n
}

func ids(nodes []domain.ProxyNodeRead) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.ID
	}
	return out
}
