package domain

import (
	"slices"
	"strings"
	"testing"
)

func TestCountryCodeAcceptsCodeEnglishAndChinese(t *testing.T) {
	for _, value := range []string{"JP", "jp", "Japan", "japan", "日本"} {
		if got := CountryCode(value); got != "JP" {
			t.Errorf("CountryCode(%q) = %q, want JP", value, got)
		}
	}
	// VPN Gate publishes the ISO long forms; the comma is optional there.
	for value, want := range map[string]string{
		"Korea Republic of":         "KR",
		"Korea, Republic of":        "KR",
		"Russian Federation":        "RU",
		"Viet Nam":                  "VN",
		"United States":             "US",
		"Iran, Islamic Republic of": "IR",
		"Hong Kong":                 "HK",
	} {
		if got := CountryCode(value); got != want {
			t.Errorf("CountryCode(%q) = %q, want %q", value, got, want)
		}
	}
	if got := CountryCode("Atlantis"); got != "" {
		t.Errorf("CountryCode(unknown) = %q, want empty", got)
	}
}

func TestCountryChineseAndFlag(t *testing.T) {
	if got := CountryChinese("KR", "Korea Republic of"); got != "韩国" {
		t.Errorf("CountryChinese = %q, want 韩国", got)
	}
	// No code and no table entry: the provider's own label is still shown.
	if got := CountryChinese("", "Atlantis"); got != "Atlantis" {
		t.Errorf("CountryChinese(unknown) = %q, want Atlantis", got)
	}
	if got := CountryFlag("JP"); got != "🇯🇵" {
		t.Errorf("CountryFlag(JP) = %q, want 🇯🇵", got)
	}
	if got := CountryFlag("ZZ"); got != "" {
		t.Errorf("CountryFlag(ZZ) = %q, want empty", got)
	}
}

func TestSameCountryAcrossLabels(t *testing.T) {
	cases := []struct {
		name, code, want string
		match            bool
	}{
		{"United States", "US", "美国", true},
		{"United States", "US", "us", true},
		{"United States", "US", "United States", true},
		{"United States", "", "美国", true},
		{"Japan", "JP", "美国", false},
		{"Japan", "JP", "", true},
		{"Atlantis", "", "Atlantis", true},
		{"Atlantis", "", "日本", false},
	}
	for _, c := range cases {
		if got := SameCountry(c.name, c.code, c.want); got != c.match {
			t.Errorf("SameCountry(%q, %q, %q) = %v, want %v", c.name, c.code, c.want, got, c.match)
		}
	}
}

func TestMatchCountryCodes(t *testing.T) {
	if got := MatchCountryCodes("日本"); !slices.Equal(got, []string{"JP"}) {
		t.Errorf("MatchCountryCodes(日本) = %v, want [JP]", got)
	}
	// A single Chinese character is a useful prefix, so substrings match.
	if got := MatchCountryCodes("韩"); !slices.Contains(got, "KR") {
		t.Errorf("MatchCountryCodes(韩) = %v, want to contain KR", got)
	}
	// English matches on a word prefix: "uni" names the United * countries...
	uni := MatchCountryCodes("uni")
	if !slices.Contains(uni, "US") || !slices.Contains(uni, "GB") || !slices.Contains(uni, "AE") {
		t.Errorf("MatchCountryCodes(uni) = %v, want US, GB and AE", uni)
	}
	// ...while an inner substring does not, or every search would hit half the table.
	if got := MatchCountryCodes("apa"); slices.Contains(got, "JP") {
		t.Errorf("MatchCountryCodes(apa) = %v, should not contain JP", got)
	}
	if got := MatchCountryCodes("kr"); !slices.Contains(got, "KR") {
		t.Errorf("MatchCountryCodes(kr) = %v, want to contain KR", got)
	}
	if got := MatchCountryCodes("  "); got != nil {
		t.Errorf("MatchCountryCodes(blank) = %v, want nil", got)
	}
}

func TestCountryTableIsConsistent(t *testing.T) {
	seen := map[string]string{}
	for code, n := range countryTable {
		if len(code) != 2 || !isASCIILetters(code) || code != strings.ToUpper(code) {
			t.Errorf("code %q is not an uppercase alpha-2 code", code)
		}
		if n.english == "" || n.chinese == "" {
			t.Errorf("%s has an empty name: %+v", code, n)
		}
		if prev, ok := seen[n.chinese]; ok {
			t.Errorf("Chinese name %q is used by both %s and %s", n.chinese, prev, code)
		}
		seen[n.chinese] = code
	}
	for alias, code := range countryAliases {
		if _, ok := countryTable[code]; !ok {
			t.Errorf("alias %q points at unknown code %q", alias, code)
		}
		if normalizeCountryName(alias) != alias {
			t.Errorf("alias %q is not in normalized form (%q)", alias, normalizeCountryName(alias))
		}
	}
	// A label that two countries both answer to would make one of them
	// unreachable by name, silently.
	owner := map[string]string{}
	for code, n := range countryTable {
		for _, label := range []string{normalizeCountryName(n.english), n.chinese} {
			if prev, ok := owner[label]; ok && prev != code {
				t.Errorf("label %q is claimed by both %s and %s", label, prev, code)
			}
			owner[label] = code
		}
	}
	for alias, code := range countryAliases {
		if prev, ok := owner[alias]; ok && prev != code {
			t.Errorf("alias %q shadows the name of %s (points at %s)", alias, prev, code)
		}
	}
}
