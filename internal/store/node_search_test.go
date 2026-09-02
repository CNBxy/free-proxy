package store

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/masteralanlab/free-proxy/internal/domain"
)

// seedSearchNodes stores two enriched nodes: the pool as the console sees it,
// where the provider's country label is English and the enrichment API's
// location is Chinese.
func seedSearchNodes(t *testing.T) *Repos {
	t.Helper()
	repos := newNodeRepos(t)
	ctx := context.Background()
	jp := node("jp1", "1.2.3.4")
	jp.Country, jp.CountryCode, jp.HostName = "Japan", "JP", "vpn926183417"
	us := node("us1", "5.6.7.8")
	us.Country, us.CountryCode, us.HostName = "United States", "US", "vpn405118822"
	if _, err := repos.Nodes.UpsertDiscovered(ctx, []domain.DiscoveredNode{jp, us}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	infos := map[string]domain.IpInfo{
		"jp1": {IPAddress: "1.2.3.4", Owner: "NTT Communications", ASN: "AS2914", ASName: "NTT-COM",
			Location: "日本 东京都 涩谷区", IPType: domain.IpResidential},
		"us1": {IPAddress: "5.6.7.8", Owner: "Comcast Cable", ASN: "AS7922", ASName: "COMCAST",
			Location: "美国 加利福尼亚州 洛杉矶", IPType: domain.IpHosting},
	}
	for id, info := range infos {
		if err := repos.Nodes.UpdateIPInfo(ctx, id, info, now); err != nil {
			t.Fatal(err)
		}
	}
	return repos
}

func searchIDs(t *testing.T, repos *Repos, query string) []string {
	t.Helper()
	nodes, err := repos.Nodes.ListNodes(context.Background(), NodeFilter{Search: query}, 50, 0)
	if err != nil {
		t.Fatalf("search %q: %v", query, err)
	}
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	return ids
}

func TestSearchMatchesChineseCountryAndLocation(t *testing.T) {
	repos := seedSearchNodes(t)
	cases := []struct {
		query string
		want  []string
	}{
		// The stored label is "Japan"; the Chinese name reaches it by code.
		{"日本", []string{"jp1"}},
		{"韩国", nil},
		{"东京", []string{"jp1"}},           // enrichment location
		{"Japan", []string{"jp1"}},        // provider label
		{"jp", []string{"jp1"}},           // alpha-2 code
		{"united", []string{"us1"}},       // word prefix of "United States"
		{"comcast", []string{"us1"}},      // owner
		{"AS2914", []string{"jp1"}},       // ASN
		{"1.2.3.4", []string{"jp1"}},      // address
		{"vpn405118822", []string{"us1"}}, // provider host name
		// Terms are ANDed, so a second word narrows instead of widening.
		{"美国 comcast", []string{"us1"}},
		{"美国 NTT", nil},
		{"  日本   NTT ", []string{"jp1"}},
		// A Chinese IME types these separators as readily as a space.
		{"日本，东京", []string{"jp1"}},
		{"日本、NTT", []string{"jp1"}},
		{"日本　NTT", []string{"jp1"}}, // U+3000 ideographic space
		// Wildcards are the user's literal text, not LIKE syntax.
		{"%", nil},
		{"_", nil},
	}
	for _, c := range cases {
		got := searchIDs(t, repos, c.query)
		if len(got) != len(c.want) {
			t.Errorf("search %q = %v, want %v", c.query, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("search %q = %v, want %v", c.query, got, c.want)
				break
			}
		}
	}
}

func TestSearchTermsSplitsAndBounds(t *testing.T) {
	got := searchTerms(" 日本，东京、NTT ; jp | x　y ")
	want := []string{"日本", "东京", "NTT", "jp", "x", "y"}
	if !slices.Equal(got, want) {
		t.Errorf("searchTerms = %q, want %q", got, want)
	}
	// Truncation counts runes: cutting a Chinese term mid-character would build
	// a pattern that can never match.
	long := searchTerms(strings.Repeat("日", 100))
	if len(long) != 1 || utf8.RuneCountInString(long[0]) != maxSearchTermRunes || !utf8.ValidString(long[0]) {
		t.Errorf("long term = %q (%d runes, valid=%v)", long, utf8.RuneCountInString(long[0]), utf8.ValidString(long[0]))
	}
	if terms := searchTerms("a b c d e f g h i"); len(terms) != maxSearchTerms {
		t.Errorf("term cap = %d, want %d", len(terms), maxSearchTerms)
	}
}

func TestSearchMatchesIDByPrefixOnly(t *testing.T) {
	repos := newNodeRepos(t)
	n := node("jp-a1b2deadbeef", "9.9.9.9")
	n.Country, n.CountryCode = "Japan", "JP"
	if _, err := repos.Nodes.UpsertDiscovered(context.Background(), []domain.DiscoveredNode{n}); err != nil {
		t.Fatal(err)
	}
	if got := searchIDs(t, repos, "jp-a1b2"); len(got) != 1 {
		t.Errorf("id prefix search = %v, want the node", got)
	}
	// An id is a hex digest: matching it anywhere would answer everyday words
	// like "beef" or "added" with unrelated nodes.
	if got := searchIDs(t, repos, "deadbeef"); len(got) != 0 {
		t.Errorf("id substring search = %v, want nothing", got)
	}
}

func TestSearchCountsMatchList(t *testing.T) {
	repos := seedSearchNodes(t)
	total, err := repos.Nodes.CountNodes(context.Background(), NodeFilter{Search: "日本"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("CountNodes = %d, want 1", total)
	}
}

func TestListNodesCarriesChineseNameAndFlag(t *testing.T) {
	repos := seedSearchNodes(t)
	nodes, err := repos.Nodes.ListNodes(context.Background(), NodeFilter{Country: "日本"}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].ID != "jp1" {
		t.Fatalf("country filter = %+v, want jp1", nodes)
	}
	if nodes[0].CountryZH != "日本" || nodes[0].CountryFlag != "🇯🇵" {
		t.Fatalf("display fields = %q/%q, want 日本/🇯🇵", nodes[0].CountryZH, nodes[0].CountryFlag)
	}
}

func TestCountryCounts(t *testing.T) {
	repos := seedSearchNodes(t)
	counts, err := repos.Nodes.CountryCounts(context.Background(), NodeFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 2 {
		t.Fatalf("CountryCounts = %+v, want 2 rows", counts)
	}
	byCode := map[string]domain.CountryCount{}
	for _, c := range counts {
		byCode[c.Code] = c
	}
	jp, ok := byCode["JP"]
	if !ok {
		t.Fatalf("CountryCounts = %+v, want a JP row", counts)
	}
	if jp.CountryZH != "日本" || jp.CountryFlag != "🇯🇵" || jp.Total != 1 {
		t.Fatalf("JP row = %+v", jp)
	}
	// A picked country must not narrow the picker itself.
	filtered, err := repos.Nodes.CountryCounts(context.Background(), NodeFilter{Country: "JP", Search: "日本"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 2 {
		t.Fatalf("CountryCounts ignoring country/search = %+v, want 2 rows", filtered)
	}
	// Other filters still apply.
	hosting, err := repos.Nodes.CountryCounts(context.Background(), NodeFilter{IPType: string(domain.IpHosting)})
	if err != nil {
		t.Fatal(err)
	}
	if len(hosting) != 1 || hosting[0].Code != "US" {
		t.Fatalf("hosting facet = %+v, want only US", hosting)
	}
}
