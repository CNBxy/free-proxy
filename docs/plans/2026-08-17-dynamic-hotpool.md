# 动态代理热池 Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** 对外仍走同一 `:9527`，对内预连最多 10 条隧道；轮询按新 TCP 随机选槽，填充粘住后到期或手动 API 整池更换；grok2api 所有非粘滞 `proxyPool` 走 `freshTunnel`。

**Architecture:** HotPool 管槽位（选节点 / 建拆 / LRU / 填充窗口 / rotate）。每槽独立 OpenVPN（`dyn0–dyn10`）+ 独立策略表（110–120）。本地代理 `Dial` 向 HotPool 取 ready 槽并 `SO_BINDTODEVICE`。动态开时停 tun0 单出口与切最优。管理 API 增加 Bearer 管理员密码。grok2api 去掉 `freshTunnel` 的 Build-only 限制。

**Tech Stack:** Go 1.23、Echo v5、sqlc + goose/SQLite、React 19。假 Runner / 假 Connector，不真起 OpenVPN。

**Design:** `docs/plans/2026-08-17-dynamic-hotpool-design.md`（已冻结，`dbbb067`）。

**Repos:**
- free-proxy：`/root/free-proxy`，分支 `develop/Dynamic_Proxy`
- grok2api：从 `origin/develop/20260814` 开 `feat/free-proxy-fresh-tunnel`（**独立 worktree**，禁止碰 `/root/Grok/grok2api` 当前脏工作区）

**约束:**
- 中文 commit / 回复
- TDD：先红后绿；最小 diff
- 不改探测 `tun2–99`；热池只用 `dyn0–dyn10`
- 不按 URL 猜 free-proxy；不新开 token / 端口
- grok2api 粘滞账号（`freshTunnel=false`）仍复用连接

---

### Task 1: 领域枚举与设置字段

**Objective:** 让编译期类型能表达动态开关、规则、填充时长。

**Files:**
- Modify: `internal/domain/enums.go`
- Modify: `internal/domain/models.go`
- Modify: `internal/domain/errors.go`
- Test: `internal/domain/dynamic_settings_test.go`（新建）

**Step 1: Write failing test**

```go
package domain

import "testing"

func TestNormalizeStickyDuration(t *testing.T) {
	sec, err := NormalizeStickyDuration(5, DurationUnitMinute)
	if err != nil || sec != 300 {
		t.Fatalf("5m = %d, %v; want 300", sec, err)
	}
	sec, err = NormalizeStickyDuration(45, DurationUnitSecond)
	if err != nil || sec != 45 {
		t.Fatalf("45s = %d, %v; want 45", sec, err)
	}
	if _, err := NormalizeStickyDuration(10, DurationUnitSecond); err == nil {
		t.Fatal("10s should be below minimum")
	}
	if _, err := NormalizeStickyDuration(2000, DurationUnitMinute); err == nil {
		t.Fatal("2000m should exceed max")
	}
}
```

**Step 2: Run test to verify failure**

```bash
cd /root/free-proxy && go test ./internal/domain -run TestNormalizeStickyDuration -count=1
```

Expected: FAIL — `NormalizeStickyDuration` undefined.

**Step 3: Write minimal implementation**

`enums.go` 追加：

```go
type DynamicRule string

const (
	DynamicRotate DynamicRule = "rotate"
	DynamicSticky DynamicRule = "sticky"
)

type DurationUnit string

const (
	DurationUnitSecond DurationUnit = "s"
	DurationUnitMinute DurationUnit = "m"
)
```

`models.go` 的 `ProxySettings` / `ProxySettingsUpdate` 追加：

```go
DynamicEnabled         bool         `json:"dynamic_enabled"`
DynamicRule            DynamicRule  `json:"dynamic_rule"`
StickyDurationSeconds  int          `json:"sticky_duration_seconds"`
StickyDurationUnit     DurationUnit `json:"sticky_duration_unit"`
```

`errors.go` 追加：

```go
ErrDynamicDisabled   = errors.New("dynamic proxy is disabled")
ErrRotateNotSticky   = errors.New("rotate is only available in sticky mode")
ErrNoHotPoolSlot     = errors.New("no ready hot-pool slot")
ErrRotateInProgress  = errors.New("hot-pool rotate already running")
```

同文件实现：

```go
func NormalizeStickyDuration(value int, unit DurationUnit) (int, error) {
	seconds := value
	if unit == DurationUnitMinute {
		seconds = value * 60
	}
	if seconds < 30 || seconds > 86400 {
		return 0, errors.New("sticky duration must be between 30s and 86400s")
	}
	return seconds, nil
}
```

**Step 4: Run test to verify pass**

```bash
go test ./internal/domain -run TestNormalizeStickyDuration -count=1
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/domain && git commit -m "feat: 增加动态代理设置字段与时长换算"
```

---

### Task 2: SQLite 迁移 + sqlc

**Objective:** `runtime_settings` 持久化动态字段；Get/Update 读写它们。

**Files:**
- Create: `internal/store/migrations/0004_dynamic_proxy.sql`
- Modify: `internal/store/queries/settings.sql`
- Modify: `internal/store/repo.go`（Get/Update 映射）
- Test: `internal/store/dynamic_settings_test.go`

**Step 1: Write failing test**（先写测试，迁移未加时 `Get` 不含新字段）

```go
package store

func TestRuntimeSettingsRoundTripDynamicFields(t *testing.T) {
	db, err := Open("file:" + filepath.Join(t.TempDir(), "dyn.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := Migrate(db); err != nil { t.Fatal(err) }
	repo := NewRepos(db).Settings
	ctx := context.Background()
	got, err := repo.Get(ctx)
	if err != nil { t.Fatal(err) }
	if got.DynamicEnabled || got.DynamicRule != domain.DynamicRotate || got.StickyDurationSeconds != 300 || got.StickyDurationUnit != domain.DurationUnitMinute {
		t.Fatalf("defaults = %+v", got)
	}
	err = repo.Update(ctx, domain.ProxySettingsUpdate{
		RoutingMode: domain.PolicyAuto, RoutingIPType: domain.RoutingAll,
		ConnectionEnabled: true, DynamicEnabled: true, DynamicRule: domain.DynamicSticky,
		StickyDurationSeconds: 600, StickyDurationUnit: domain.DurationUnitSecond,
	})
	if err != nil { t.Fatal(err) }
	got, err = repo.Get(ctx)
	if err != nil { t.Fatal(err) }
	if !got.DynamicEnabled || got.DynamicRule != domain.DynamicSticky || got.StickyDurationSeconds != 600 || got.StickyDurationUnit != domain.DurationUnitSecond {
		t.Fatalf("updated = %+v", got)
	}
}
```

**Step 2: Run — expect FAIL**（列不存在或字段为零值）

```bash
go test ./internal/store -run TestRuntimeSettingsRoundTripDynamicFields -count=1
```

**Step 3: Migration + queries + mapping**

`0004_dynamic_proxy.sql`:

```sql
-- +goose Up
ALTER TABLE runtime_settings ADD COLUMN dynamic_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runtime_settings ADD COLUMN dynamic_rule TEXT NOT NULL DEFAULT 'rotate';
ALTER TABLE runtime_settings ADD COLUMN sticky_duration_seconds INTEGER NOT NULL DEFAULT 300;
ALTER TABLE runtime_settings ADD COLUMN sticky_duration_unit TEXT NOT NULL DEFAULT 'm';

-- +goose Down
-- SQLite cannot cheaply drop columns; leave unused on downgrade.
```

`settings.sql` 的 `UpdateRuntimeSettings` 补四列；`SELECT *` 已覆盖。

```bash
cd /root/free-proxy && make gen
```

`repo.go` `Get`/`Update` 映射这四列。空/非法 rule、unit 读出时回落到 `rotate` / `m`。

**Step 4:** 同上测试 PASS

**Step 5: Commit**

```bash
git add internal/store && git commit -m "feat: 持久化动态代理开关与调度规则"
```

---

### Task 3: 选槽纯函数（N / LRU / 分类）

**Objective:** 不碰隧道，先把「从分类里抽最多 10 个、补槽走 LRU」测绿。

**Files:**
- Create: `internal/services/hotpool_select.go`
- Test: `internal/services/hotpool_select_test.go`

**Step 1: Failing tests**

```go
func TestHotPoolSizeCapsAtTen(t *testing.T) {
	nodes := make([]domain.ProxyNodeRead, 15)
	for i := range nodes {
		nodes[i] = domain.ProxyNodeRead{ID: fmt.Sprintf("n%d", i), Status: domain.NodeReady, IPType: domain.IpHosting}
	}
	picked := PickInitialNodes(nodes, domain.ProxySettings{RoutingMode: domain.PolicyAuto, RoutingIPType: domain.RoutingAll}, nil, time.Unix(0, 0))
	if len(picked) != 10 {
		t.Fatalf("len=%d want 10", len(picked))
	}
}

func TestLRUSkipsOccupiedAndPrefersOldest(t *testing.T) {
	candidates := []domain.ProxyNodeRead{
		{ID: "occ", Status: domain.NodeReady},
		{ID: "old", Status: domain.NodeReady},
		{ID: "new", Status: domain.NodeReady},
	}
	lastUsed := map[string]time.Time{
		"old": time.Unix(1, 0),
		"new": time.Unix(100, 0),
	}
	got := PickReplacement(candidates, map[string]bool{"occ": true}, lastUsed, time.Unix(200, 0))
	if got == nil || got.ID != "old" {
		t.Fatalf("got %+v, want old", got)
	}
}
```

**Step 2:** `go test ./internal/services -run 'TestHotPoolSize|TestLRU' -count=1` → FAIL

**Step 3:** 实现

```go
const HotPoolMax = 10

func PickInitialNodes(nodes []domain.ProxyNodeRead, settings domain.ProxySettings, lastUsed map[string]time.Time, now time.Time) []domain.ProxyNodeRead {
	filtered := ApplyFilters(nodes, settings, false)
	// 只保留 ready；按 lastUsed 升序（零值=从未用过，最优先），同分按 ID
	// 截断到 HotPoolMax
}

func PickReplacement(nodes []domain.ProxyNodeRead, occupied map[string]bool, lastUsed map[string]time.Time, now time.Time) *domain.ProxyNodeRead {
	// 排除 occupied；从未用过优先，否则 lastUsed 最旧
}
```

**Step 4:** PASS  
**Step 5:** `git commit -m "feat: 热池按分类截断并按 LRU 补槽"`

---

### Task 4: HotPool 状态机（假隧道）

**Objective:** 用接口把建/拆隧道注入，测轮询换槽、填充粘住、rotate 门闩。

**Files:**
- Create: `internal/services/hotpool.go`
- Test: `internal/services/hotpool_test.go`

槽状态：`connecting | ready | draining | dead`

接口（测试用假实现）：

```go
type SlotTunnel interface {
	Start(ctx context.Context, nodeID, configText, device string, table int) error
	Stop(device string)
}
```

假实现：`Start` 记调用并立刻成功；可配置某 nodeID 失败。

**Step 1: Tests**

1. `TestSelectSlotEmpty` → `ErrNoHotPoolSlot`
2. `TestSelectSlotSkipsDraining`：一条 ready、一条 draining，只返回 ready
3. `TestRotateReplaceWaitsForWarmup`：`OnConnClosed` 在轮询+候选>10 时先 `Start` 新槽，成功后旧槽才 `draining`；`Start` 失败则旧槽仍 `ready`
4. `TestStickyDoesNotReplaceInsideWindow`：sticky + now < expire，`OnConnClosed` 不 Start
5. `TestStickyExpiresThenReplaces`：now 过期后 `OnConnClosed` 触发 replace
6. `TestRotateAllRequiresSticky`：rule=rotate → `ErrRotateNotSticky`；disabled → `ErrDynamicDisabled`
7. `TestRotateAllConflict`：进行中再调 → `ErrRotateInProgress`（可用阻塞的 fake Start）

**Step 2:** 红  
**Step 3:** `HotPool` 结构：

```go
type HotPool struct {
	mu sync.Mutex
	slots []*Slot
	rule domain.DynamicRule
	enabled bool
	stickyUntil time.Time
	rotating bool
	tunnels SlotTunnel
	clock func() time.Time
	lastUsed map[string]time.Time
}
```

- `SelectSlot()` 只返回 ready 且非 draining
- `OnConnClosed(id)` inflight--；rotate 规则且分类>10 → `replaceSlot`
- `replaceSlot`：预连（允许 len==11）成功才把旧槽标 draining
- `RotateAll` 检查门闩后对每个槽 replace
- `Reconcile(settings, candidates)`：开关/分类变化
- `Stop()`：全部 draining → Stop 隧道

设备：`dyn0`…`dyn9`，临时 `dyn10`。表：`110+index`。

**Step 4:** 绿  
**Step 5:** `git commit -m "feat: 热池槽位状态机与轮询/填充/rotate"`

---

### Task 5: 多实例隧道 + 每槽路由

**Objective:** 真代码能按 device 起/停 OpenVPN，且 Cleanup 只拆自己的 table。

**Files:**
- Create: `internal/tunnel/instance.go`
- Modify: `internal/tunnel/openvpn.go`（抽出 `startOnDevice`，Manager.Connect 仍走 tun0）
- Modify: `internal/netx/routing.go`（Cleanup 只删该实例的 table/rule，不要误伤其它表）
- Test: `internal/tunnel/instance_test.go`、扩展 `internal/netx` 若已有 command runner 测试

**注意:** `Manager.Connect` 必须继续 `disconnectLocked` 单出口，热池**禁止**走 `Connect`。

`Instance`：

```go
func (m *Manager) StartInstance(ctx context.Context, nodeID, configText, device string) (domain.TunnelStartResult, *Managed, error)
func (m *Manager) StopInstance(managed *Managed)
```

`StartInstance` 用 `RouteNoPull: true`、独立 ovpn 文件（prefix=`dyn`），**不**改 `m.active`。

PolicyRouter：构造时绑定 table；`Cleanup` 只 `ip rule del table T` + `ip route flush table T`。现网单出口仍用 table 100，行为不变。给 `Cleanup` 加测试：两次 Setup 不同 table，清 A 不清 B（假 Runner 记录命令）。

**Commit:** `feat: 热池隧道按网卡独立启停并隔离路由表`

---

### Task 6: 代理 Dial 绑定热池槽

**Objective:** `:9527` 每条新 TCP 随机绑一条 ready tun。

**Files:**
- Modify: `internal/proxy/connector.go`（增加可选 `SlotSelector`）
- Modify: `internal/proxy/dns.go` 如需按槽 DNS
- Test: `internal/proxy/hotpool_connector_test.go`

```go
type SlotSelector interface {
	SelectSlot() (iface string, release func(), err error)
}
```

`HotPool.SelectSlot` 返回 iface，并在 `release` 里 `OnConnClosed`。

`SocketConnector.Dial`：
- 有 selector：先选槽，失败返回 `ErrNoHotPoolSlot`；`bindControl(iface)` 拨号；defer release
- 无 selector：保持现网绑定 `cfg.TunnelInterface`

测试用假 selector + `net.Pipe` 或记录 iface 的 Dialer Control 替身（可不真 SO_BINDTODEVICE）：selector 返回 `"dyn3"`，connector 调用一次并在 Dial 返回后 release。0 slot → 错误。

接线稍后 Task 8：动态开时把 selector 交给 gateway 的 proxy。

**Commit:** `feat: 本地代理按热池槽位绑定出口网卡`

---

### Task 7: Bearer 管理员密码 + rotate API

**Objective:** Cookie 或 `Authorization: Bearer <管理员密码>`；rotate 仅 sticky。

**Files:**
- Modify: `internal/api/middleware.go`
- Modify: `internal/security/security.go`（`VerifyPasswordOnly` 或复用：Bearer 用当前 username + token 当 password）
- Modify: `internal/api/server.go`（注册路由）
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/deps.go`（HotPool）
- Test: `internal/api/auth_bearer_test.go`、`internal/api/rotate_handler_test.go`

Bearer 校验：

```go
func bearerOK(auth *security.AuthService, header string) bool {
	token := strings.TrimPrefix(header, "Bearer ")
	u := auth.Store.Config().Username
	return auth.Verify(u, token)
}
```

`SecretPath`：`authed = sessionValid || bearerOK`。

Handler `DynamicRotate`：
- settings 关 → 400 `ErrDynamicDisabled`
- rule != sticky → 400 `ErrRotateNotSticky`
- rotating → 409 `ErrRotateInProgress`
- 否则 `Jobs.Submit(..., "rotate-hotpool", hotpool.RotateAllJob)`

`errorHandler` 把上述 sentinel 映到 400/409。

测试可构造 Echo + 假 Auth/HotPool，不必起全服。

**Commit:** `feat: 动态切换 API 支持 Cookie 与管理员 Bearer`

---

### Task 8: 与单出口互斥并接线

**Objective:** 动态开停 tun0 / AutoSwitch / Health 切最优；关则拆热池恢复现状。

**Files:**
- Modify: `internal/services/settings.go`（Update 后 `hotpool.Reconcile`；动态开时不 `Activate` tun0）
- Modify: `internal/services/autoswitch.go`（dynamic 开：Switch 转空或只通知 HotPool 补死槽）
- Modify: `internal/services/health.go`（dynamic 开：不 rotate 最优）
- Modify: `internal/services/maintenance.go`（dynamic 开：不 Activate tun0）
- Modify: `internal/services/gateway.go`（Status 附加 hotpool 快照；Activate 在 dynamic 开时返回 400）
- Modify: `cmd/free-proxy/serve.go`（创建 HotPool、selector、启动 Reconcile）
- Test: `internal/services/dynamic_gate_test.go`

`GatewayStatus` 加：

```go
DynamicEnabled bool           `json:"dynamic_enabled"`
HotPool        []HotPoolSlot  `json:"hotpool,omitempty"`
```

`HotPoolSlot`：`node_id, iface, table, status, inflight, exit_ip`。

手动 `POST /proxies/{id}/activate` 在 dynamic 开时 400（YAGNI）。

serve.go：动态开时 connector 用 selector；关时回 tun0 connector。可用 `proxy.Gateway` 换 connector 或启动时装一个会查 HotPool.enabled 的复合 connector。

**Commit:** `feat: 动态开停切换热池与单出口`

---

### Task 9: 设置校验 + 后台 UI

**Objective:** PUT `/settings` 校验时长；策略页能开关；网关页列槽。

**Files:**
- Modify: `internal/api/handlers.go` `UpdateSettings`
- Modify: `frontend/src/types.ts`
- Modify: `frontend/src/api.ts`（`rotateHotPool`）
- Modify: `frontend/src/components/SettingsPanel.tsx`
- Modify: `frontend/src/components/GatewayPanel.tsx`

校验：rule ∈ {rotate,sticky}；unit ∈ {s,m}；`NormalizeStickyDuration`；开启动态不改 secret path。

UI：
- 复选「开启动态代理」
- 规则：轮询 / 填充
- 填充：数字 + 秒/分钟；默认展示 5 + 分钟
- 网关：dynamic 时列出 slots；填充时「切换节点」改调 `POST /dynamic-proxy/rotate`；轮询时该按钮 disabled，提示仅填充可用

**Commit:** `feat: 后台配置动态代理并展示热池槽位`

---

### Task 10: README API 一行 + 全量测试

**Files:** `README.md` API 摘要补 `POST /api/v1/dynamic-proxy/rotate`  
（其它语言 README 本轮可不改，避免无关大翻。）

```bash
cd /root/free-proxy && go test ./... && go vet ./...
```

Expected: 全绿。

**Commit:** `docs: 补充动态切换 API`

---

### Task 11: grok2api freshTunnel 扩到 Web/Console

**Objective:** 所有非粘滞 `proxyPool` 请求关复用。

**Worktree（必须）：**

```bash
cd /root/Grok/grok2api
git fetch origin develop/20260814
git worktree add /tmp/grok2api-fresh-tunnel origin/develop/20260814 -b feat/free-proxy-fresh-tunnel
cd /tmp/grok2api-fresh-tunnel
```

禁止修改 `/root/Grok/grok2api`（有未提交的 compose / quality_guard）。

**Files:**
- Modify: `backend/internal/infra/egress/manager.go` `doRequest`：去掉 `l.Scope == domain.ScopeBuild &&`
- Modify: `backend/internal/infra/egress/sticky_retry_test.go`
- 可选：freshTunnel + browser 时 `Do` 后 `CloseIdleConnections`（仅当现有测试证明 tls-client 仍复用）

**Step 1:** 先改测试为「Web/Console + freshTunnel 必须 Close」——现实现会红。

把 `TestFixedBuildWebAndAccountBoundProxyKeepConnectionReuse` 拆开：
- fixed Build / 粘滞 Build：仍禁止 Close
- 新增 `TestProxyPoolFreshTunnelDisablesReuse`：Build + Web + Console，`freshTunnel=true` → 必须 Close

**Step 2:** 红  
**Step 3:**

```go
if l.freshTunnel {
    request.Close = true
}
```

**Step 4:**

```bash
docker run --rm -v /tmp/grok2api-fresh-tunnel/backend:/src -w /src \
  -v grok2api-gomod:/go/pkg/mod -v grok2api-gocache:/root/.cache/go-build \
  golang:1.26-alpine go test ./internal/infra/egress/ -count=1
```

**Step 5:** commit 在 worktree，**不要 push，除非用户明确要**。

```bash
git add backend/internal/infra/egress && git commit -m "feat(egress): proxyPool 全 scope 关闭连接复用"
```

---

## 手测（计划外，完成后提示用户）

1. 后台开动态 + 轮询，分类里 ≥2 个 ready；`curl --proxy socks5h://127.0.0.1:9527 https://api.ipify.org` 多次，IP 应在热池内变化（客户端不复用时）。
2. 填充 1 分钟：窗口内槽位不变；到期后更换。
3. `curl -X POST -H "Authorization: Bearer <管理员密码>" http://127.0.0.1:39527/<secret>/api/v1/dynamic-proxy/rotate` → 202；轮询模式 → 400。
4. grok2api 指向该代理的节点勾 `proxyPool`，连续两次非流式请求应是不同 CONNECT。

---

## 完成标准

- free-proxy `go test ./...` 绿
- 动态关时行为与现网一致（单出口 / AutoSwitch）
- 动态开时 `:9527` 不绑 tun0
- rotate 仅 sticky + Bearer/Cookie
- grok2api worktree 测试绿；主工作区未动
