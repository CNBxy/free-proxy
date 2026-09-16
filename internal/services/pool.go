package services

import (
	"context"
	"sort"
	"strings"

	"github.com/masteralanlab/free-proxy/internal/domain"
	"github.com/masteralanlab/free-proxy/internal/store"
)

// candidateFetchLimit bounds how many rows the selection and probe paths pull in
// one go. It sits far above the pool's steady-state size on purpose: the pool is
// no longer capped at one provider snapshot, so a limit near the expected size
// would silently truncate the candidate set instead of failing visibly.
const candidateFetchLimit = 5000

// ProxyPoolService selects and filters usable nodes per routing settings.
type ProxyPoolService struct {
	nodes    *store.NodeRepository
	settings *store.SettingsRepository
}

// NewProxyPoolService constructs a ProxyPoolService.
func NewProxyPoolService(nodes *store.NodeRepository, settings *store.SettingsRepository) *ProxyPoolService {
	return &ProxyPoolService{nodes: nodes, settings: settings}
}

// ValidateAllowed errors if the node is disallowed by current routing settings.
func (s *ProxyPoolService) ValidateAllowed(ctx context.Context, node domain.ProxyNodeRead) error {
	settings, err := s.settings.Get(ctx)
	if err != nil {
		return err
	}
	if !settings.ConnectionEnabled {
		return domain.ErrDisabled
	}
	if len(ApplyFilters([]domain.ProxyNodeRead{node}, settings, false)) == 0 {
		return domain.ErrRoutingMismatch
	}
	return nil
}

// Statistics returns pool-wide counts.
func (s *ProxyPoolService) Statistics(ctx context.Context) (domain.PoolStatistics, error) {
	return s.nodes.Statistics(ctx)
}

// ApplyFilters narrows nodes by routing mode and IP-type policy.
func ApplyFilters(nodes []domain.ProxyNodeRead, settings domain.ProxySettings, includeUnknownIPType bool) []domain.ProxyNodeRead {
	out := make([]domain.ProxyNodeRead, 0, len(nodes))
	fixedID := ""
	if settings.FixedNodeID != nil {
		fixedID = *settings.FixedNodeID
	}
	favorites := map[string]bool{}
	for _, id := range settings.FavoriteNodeIDs {
		favorites[id] = true
	}
	countryFilter := map[string]bool{}
	for _, code := range settings.CountryFilters {
		countryFilter[strings.ToUpper(code)] = true
	}
	for _, n := range nodes {
		switch settings.RoutingMode {
		case domain.PolicyFixed:
			if n.ID != fixedID {
				continue
			}
		case domain.PolicyCountry:
			if !domain.SameCountry(n.Country, n.CountryCode, settings.ForceCountry) {
				continue
			}
		case domain.PolicyFavorites:
			if !favorites[n.ID] {
				continue
			}
		}
		if len(countryFilter) > 0 && !countryFilter[strings.ToUpper(n.CountryCode)] {
			continue
		}
		switch settings.RoutingIPType {
		case domain.RoutingResidential:
			if !(n.IPType == domain.IpResidential || n.IPType == domain.IpMobile ||
				(includeUnknownIPType && n.IPType == domain.IpUnknown)) {
				continue
			}
		case domain.RoutingHosting:
			if !(n.IPType == domain.IpHosting || (includeUnknownIPType && n.IPType == domain.IpUnknown)) {
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

// SortCandidates orders nodes by the effective selection key for settings.
func SortCandidates(nodes []domain.ProxyNodeRead, settings domain.ProxySettings) {
	if len(settings.PriorityOrder) > 0 {
		SortCandidatesByPriority(nodes, settings.PriorityOrder)
		return
	}
	if settings.RoutingMode == domain.PolicySmart {
		sortSmartCandidates(nodes, false)
		return
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return lessFor(nodes[i], nodes[j], settings)
	})
}

// sortSmartCandidates ranks the pool using relative scores so latency,
// advertised speed, and VPN Gate session count contribute without their raw
// units overwhelming one another. Latency and speed carry 40% each; lower
// session count carries the remaining 20%.
func sortSmartCandidates(nodes []domain.ProxyNodeRead, sourceLatency bool) {
	if len(nodes) < 2 {
		return
	}
	latencies := make([]float64, len(nodes))
	speeds := make([]float64, len(nodes))
	sessions := make([]float64, len(nodes))
	for i, n := range nodes {
		latencies[i] = smartLatency(n, sourceLatency)
		speeds[i] = float64(effSpeed(n))
		sessions[i] = float64(n.SourceSessions)
	}
	minMax := func(values []float64) (float64, float64) {
		min, max := values[0], values[0]
		for _, value := range values[1:] {
			if value < min {
				min = value
			}
			if value > max {
				max = value
			}
		}
		return min, max
	}
	latMin, latMax := minMax(latencies)
	speedMin, speedMax := minMax(speeds)
	sessionsMin, sessionsMax := minMax(sessions)
	normalize := func(value, min, max float64) float64 {
		if max == min {
			return 0.5
		}
		return (value - min) / (max - min)
	}
	scores := make(map[string]float64, len(nodes))
	for i, n := range nodes {
		latencyScore := 1 - normalize(latencies[i], latMin, latMax)
		speedScore := normalize(speeds[i], speedMin, speedMax)
		sessionScore := 1 - normalize(sessions[i], sessionsMin, sessionsMax)
		scores[n.ID] = 0.4*latencyScore + 0.4*speedScore + 0.2*sessionScore
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		if scores[nodes[i].ID] != scores[nodes[j].ID] {
			return scores[nodes[i].ID] > scores[nodes[j].ID]
		}
		return nodes[i].ID < nodes[j].ID
	})
}

func smartLatency(n domain.ProxyNodeRead, sourceLatency bool) float64 {
	if n.LatencyMS > 0 {
		return float64(n.LatencyMS)
	}
	if sourceLatency && n.SourcePingMS > 0 {
		return float64(n.SourcePingMS)
	}
	return 999999
}

func residentialRank(n domain.ProxyNodeRead) int {
	if n.IPType == domain.IpResidential || n.IPType == domain.IpMobile {
		return 0
	}
	return 1
}

func effLatency(n domain.ProxyNodeRead) int {
	if n.LatencyMS > 0 {
		return n.LatencyMS
	}
	return 999999
}

func effSpeed(n domain.ProxyNodeRead) int64 {
	if n.SourceSpeedBPS > 0 {
		return n.SourceSpeedBPS
	}
	return -1
}

func lessFor(a, b domain.ProxyNodeRead, settings domain.ProxySettings) bool {
	if settings.RoutingMode == domain.PolicySpeedFirst {
		if sa, sb := effSpeed(a), effSpeed(b); sa != sb {
			return sa > sb
		}
		if la, lb := effLatency(a), effLatency(b); la != lb {
			return la < lb
		}
		if a.SourceScore != b.SourceScore {
			return a.SourceScore > b.SourceScore
		}
		return residentialRank(a) < residentialRank(b)
	}
	if settings.RoutingMode == domain.PolicyResidentialFirst {
		if ra, rb := residentialRank(a), residentialRank(b); ra != rb {
			return ra < rb
		}
		if la, lb := effLatency(a), effLatency(b); la != lb {
			return la < lb
		}
		if a.SourceScore != b.SourceScore {
			return a.SourceScore > b.SourceScore
		}
		return a.SourceSpeedBPS > b.SourceSpeedBPS
	}
	if la, lb := effLatency(a), effLatency(b); la != lb {
		return la < lb
	}
	if a.SourceScore != b.SourceScore {
		return a.SourceScore > b.SourceScore
	}
	if a.SourceSpeedBPS != b.SourceSpeedBPS {
		return a.SourceSpeedBPS > b.SourceSpeedBPS
	}
	return residentialRank(a) < residentialRank(b)
}

func scoreNode(n domain.ProxyNodeRead, priorities []domain.PriorityOrder) float64 {
	totalScore := 0.0
	for _, p := range priorities {
		var rawValue float64
		switch p.Metric {
		case domain.PrioritySessions:
			rawValue = float64(n.SourceSessions)
		case domain.PriorityLatency:
			if n.LatencyMS > 0 {
				rawValue = float64(n.LatencyMS)
			} else {
				rawValue = 999999
			}
		case domain.PriorityPing:
			if n.SourcePingMS > 0 {
				rawValue = float64(n.SourcePingMS)
			} else {
				rawValue = 999999
			}
		case domain.PrioritySpeed:
			if n.SourceSpeedBPS > 0 {
				rawValue = float64(n.SourceSpeedBPS) / 1000000.0
			} else {
				rawValue = 0
			}
		}
		qualityScore := findQualityScore(rawValue, p.QualityMS)
		totalScore += p.Weight * qualityScore
	}
	return totalScore
}

func findQualityScore(value float64, brackets []domain.QualityBracket) float64 {
	intVal := int(value)
	for _, b := range brackets {
		if intVal >= b.Min && intVal <= b.Max {
			return b.Score
		}
	}
	return 0
}

func SortCandidatesByPriority(nodes []domain.ProxyNodeRead, priorities []domain.PriorityOrder) {
	if len(nodes) < 2 {
		return
	}
	scores := make(map[string]float64, len(nodes))
	for _, n := range nodes {
		scores[n.ID] = scoreNode(n, priorities)
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		if scores[nodes[i].ID] != scores[nodes[j].ID] {
			return scores[nodes[i].ID] > scores[nodes[j].ID]
		}
		return nodes[i].ID < nodes[j].ID
	})
}
