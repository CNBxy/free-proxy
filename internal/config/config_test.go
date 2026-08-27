package config

import (
	"strings"
	"testing"

	"github.com/masteralanlab/free-proxy/internal/naming"
)

func loadWith(t *testing.T, env map[string]string) (*Config, error) {
	t.Helper()
	t.Setenv("FREE_PROXY_ENV_FILE", t.TempDir()+"/absent.env") // ignore any host config
	t.Setenv("FREE_PROXY_DATA_DIR", t.TempDir())
	for k, v := range env {
		t.Setenv(k, v)
	}
	return Load()
}

// The shipped defaults must land in the project's private namespace, not in the
// shared pools every other tunnel program also defaults to (issue #2).
func TestDefaultsUsePrivateNamespace(t *testing.T) {
	cfg, err := loadWith(t, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TunnelInterface != naming.ActiveDevice() {
		t.Errorf("TunnelInterface = %q, want %q", cfg.TunnelInterface, naming.ActiveDevice())
	}
	if cfg.ProbeDevicePrefix != naming.DevicePrefix {
		t.Errorf("ProbeDevicePrefix = %q, want %q", cfg.ProbeDevicePrefix, naming.DevicePrefix)
	}
	if cfg.PolicyRoutingTable != naming.DefaultRoutingTable {
		t.Errorf("PolicyRoutingTable = %d, want %d", cfg.PolicyRoutingTable, naming.DefaultRoutingTable)
	}
	if naming.HasDevicePrefix(cfg.TunnelInterface, naming.LegacyDevicePrefix) {
		t.Errorf("default tunnel interface %q is in the shared tun* pool", cfg.TunnelInterface)
	}
}

func TestOperatorOverridesAreHonoured(t *testing.T) {
	cfg, err := loadWith(t, map[string]string{
		"FREE_PROXY_TUNNEL_INTERFACE":     "tun9",
		"FREE_PROXY_PROBE_DEVICE_PREFIX":  "myvpn",
		"FREE_PROXY_POLICY_ROUTING_TABLE": "220",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TunnelInterface != "tun9" || cfg.ProbeDevicePrefix != "myvpn" || cfg.PolicyRoutingTable != 220 {
		t.Errorf("overrides not applied: %+v", *cfg)
	}
}

func TestInvalidNamingIsRejectedAtLoad(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"reserved table", map[string]string{"FREE_PROXY_POLICY_ROUTING_TABLE": "254"}, "reserved"},
		{"long device", map[string]string{"FREE_PROXY_TUNNEL_INTERFACE": strings.Repeat("a", 20)}, "kernel limit"},
		{"device with space", map[string]string{"FREE_PROXY_TUNNEL_INTERFACE": "bad name"}, "characters"},
		{"long prefix", map[string]string{"FREE_PROXY_PROBE_DEVICE_PREFIX": strings.Repeat("p", 20)}, "too long"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := loadWith(t, c.env)
			if err == nil {
				t.Fatal("expected Load to fail")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

// The live exit's device must never be inside the probe pool, or a probe would
// be handed the name the active tunnel already holds. The range is a constant
// now, so the only way they can collide is an operator moving the tunnel onto a
// probe index — and then it is the pool that has to yield.
func TestActiveDeviceIsExcludedFromProbeRange(t *testing.T) {
	cfg, err := loadWith(t, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TestTunStart != ProbeDeviceRangeStart || cfg.TestTunEnd != ProbeDeviceRangeEnd {
		t.Errorf("probe range = %d-%d, want the constants %d-%d",
			cfg.TestTunStart, cfg.TestTunEnd, ProbeDeviceRangeStart, ProbeDeviceRangeEnd)
	}

	moved, err := loadWith(t, map[string]string{"FREE_PROXY_TUNNEL_INTERFACE": naming.DevicePrefix + "5"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if moved.TestTunStart != 6 {
		t.Errorf("TestTunStart = %d, want 6 so the probe pool clears %s", moved.TestTunStart, moved.TunnelInterface)
	}
}
