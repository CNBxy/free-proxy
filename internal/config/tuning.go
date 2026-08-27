package config

import "time"

// Tuning values that are deliberately not configurable.
//
// These were settings once — environment variables, then rows in SQLite behind
// a form with thirty-five inputs. Almost none of them were ever changed, and the
// few that were changed were changed wrongly: a probe concurrency that outruns
// the tun device pool, a maintenance interval short enough that one cycle never
// finishes before the next begins, a connect timeout below the time an OpenVPN
// handshake takes. Every one of those is a value this code has to reason about,
// and reasoning about a number the operator can move is strictly harder than
// reasoning about a number it cannot.
//
// So they live here instead, as one list, next to the reason each holds the
// value it holds. What remains configurable is what an operator genuinely has to
// decide — identity (username, password, management path) and where the service
// listens (ports, external access). Those stay in the web console; everything
// here is the program's own business.
const (
	// AppName is the display name reported by the status endpoint.
	AppName = "Free Proxy"

	// SessionTTL is how long a dashboard login lasts. Long, because this is a
	// single-operator tool behind a secret path, and re-authenticating costs the
	// one person who uses it far more than the marginal risk buys.
	SessionTTL = 30 * 24 * time.Hour

	// ProxyMaxConnections bounds concurrent client connections. A browser opens
	// dozens per page load, so this is generous; the ceiling exists to stop one
	// runaway client from exhausting the host's descriptors.
	ProxyMaxConnections = 256

	// ProxyConnectTimeout bounds the dial to the destination through the tunnel.
	// Slower than a local dial should ever need, because the packet goes through
	// a volunteer VPN exit that may be on the other side of the planet.
	ProxyConnectTimeout = 20 * time.Second

	// ProxyIdleTimeout closes connections nothing has flowed through. Keep-alive
	// pools on the far side commonly hold a minute, so this must be longer.
	ProxyIdleTimeout = 2 * time.Minute

	// ProxyDNSServer resolves destination names for proxied connections. It is
	// queried through the tunnel, so it must be a public resolver rather than
	// whatever the host itself uses — a LAN resolver would not be reachable from
	// the exit, and would leak the destination to the local network.
	ProxyDNSServer = "8.8.8.8"

	// OpenVPNUsername and OpenVPNPassword are what VPN Gate expects. Both are
	// literally "vpn" for every server in the pool; the protocol requires the
	// exchange, the values carry no meaning.
	OpenVPNUsername = "vpn"
	OpenVPNPassword = "vpn"

	// OpenVPNTestTimeout bounds a probe's handshake. Short on purpose: a probe
	// that has not connected in 15s is competing with hundreds of others for the
	// pass, and a slow exit is not one worth having.
	OpenVPNTestTimeout = 15 * time.Second

	// OpenVPNConnectTimeout bounds the live exit's handshake. Longer than the
	// probe's, because this one node has already been chosen and giving up on it
	// means falling back to a worse one.
	OpenVPNConnectTimeout = 35 * time.Second

	// MaxProbeConcurrency is how many probes run at once. Each holds a tun
	// device, an OpenVPN process and a route, so the useful ceiling is set by the
	// host's network stack rather than by its CPUs.
	MaxProbeConcurrency = 5

	// VPNGateAPIURL is the node source. IPInfoAPIURL enriches nodes with country
	// and connection type; its query string names the exact fields parsed on the
	// other end, so the two travel together.
	VPNGateAPIURL = "https://www.vpngate.net/api/iphone/"
	IPInfoAPIURL  = "http://ip-api.com/batch?lang=zh-CN&fields=status,message,query,country,regionName,city,isp,org,as,asname,proxy,hosting,mobile"

	// DiscoveryLimit caps how many nodes one provider fetch keeps. VPN Gate
	// publishes a few hundred at a time and the tail is mostly unusable.
	DiscoveryLimit = 300

	// RequestTimeout bounds provider and IP-info HTTP calls.
	RequestTimeout = 15 * time.Second

	// IPInfoCacheTTL is how long enrichment is reused. A node's country and
	// connection type are properties of the host, not of the moment.
	IPInfoCacheTTL = 7 * 24 * time.Hour

	// MaintenanceInterval is the gap between full maintenance cycles: discovery,
	// a probe pass, blacklist expiry, history pruning. Hours, not minutes — a
	// cycle holds the operation lock while it runs, and the pool does not change
	// meaningfully faster than this.
	MaintenanceInterval = 3 * time.Hour

	// DisconnectedRetry replaces MaintenanceInterval while there is no exit. With
	// nothing connected there is nothing to protect, so the loop comes back
	// quickly instead of leaving the service down for hours.
	DisconnectedRetry = 30 * time.Second

	// HealthCheckInterval is how often the live exit is verified end to end
	// through the proxy listener, and ActivePingInterval how often its latency is
	// sampled. The health check is the recovery path, so it also bounds how long
	// a dead exit can stay selected.
	HealthCheckInterval = 30 * time.Second
	ActivePingInterval  = 10 * time.Second

	// InitialConnectTestLimit is how many candidates a first run probes before
	// connecting, so a fresh install reaches a working exit without waiting for a
	// full pass. ManualTestNodeLimit bounds a hand-triggered probe request, which
	// competes with the same tun devices as everything else.
	InitialConnectTestLimit = 10
	ManualTestNodeLimit     = 5

	// InvalidBackoff is how long a node that failed to activate is skipped.
	// Long enough that a broken node stops being reselected, short enough that a
	// transient failure does not retire it for the day.
	InvalidBackoff = 30 * time.Minute

	// StaleNodeGrace is how long a node absent from the provider is kept. VPN
	// Gate's list churns constantly and a node missing from one fetch is usually
	// back in the next; a week is well past that.
	StaleNodeGrace = 7 * 24 * time.Hour

	// RoutingSetupRetries and RoutingRetryInterval cover the window after a
	// tunnel comes up where the device exists but the kernel has not finished
	// with it, and an `ip rule` lands on nothing.
	RoutingSetupRetries  = 3
	RoutingRetryInterval = time.Second

	// RoutingStrictRPFilter leaves reverse-path filtering strict when set. It is
	// off because the proxy's replies arrive on the tunnel while the route to the
	// client points out the main interface — exactly the asymmetry strict mode
	// drops.
	RoutingStrictRPFilter = false

	// ProbeDeviceRangeStart and ProbeDeviceRangeEnd bound the probe tun pool.
	// They belong to the device-namespace family in internal/naming: the range
	// has to be wide enough for MaxProbeConcurrency and must not overlap the live
	// tunnel's device.
	ProbeDeviceRangeStart = 1
	ProbeDeviceRangeEnd   = 64
)

// DNSRepairServers are written to the default interface when the operator asks
// for a DNS repair from the dashboard. Two public resolvers, because the repair
// exists precisely for hosts whose configured resolver has stopped answering.
var DNSRepairServers = []string{"1.1.1.1", "8.8.8.8"}
