# 动态代理热池设计

日期：2026-08-17
分支：`develop/Dynamic_Proxy`
状态：已冻结（grill 确认）

## 背景

free-proxy 当前是**单隧道出口**：本地 SOCKS5/HTTP（默认 `:9527`）共用一条 OpenVPN（`tun0` + 策略路由表 100）。换出口 IP 必须重连，约 15–35 秒。现有 `AutoSwitch` 在故障时切「策略最优」节点，不是按请求换 IP。

目标：开启动态代理后，**对外仍是同一个代理端口**，对内预连最多 10 条隧道，每条客户端新 TCP 随机走其中一条，从而换出口 IP。主要适配 grok2api：每次上游请求换 IP；单次流式输出整段不中断。

## 已定决策

1. 热池，不是单出口定时轮换。
2. `N = min(分类 ready 节点数, 10)`。
3. 分类 = 现有路由策略过滤后的 ready 节点（`routing_mode` / `routing_ip_type` / `force_country` / `favorites`）。
4. 轮询：每条新 TCP 在 ready 槽中随机。分类 >10 时，槽上有连接结束 → 先预连 LRU 未占用 IP（短暂允许 11 条）→ 成功后再让旧槽停接并排空。
5. 填充：持续时间内槽位粘住；新 TCP 仍在这 N 条里随机。到期后各槽按 LRU 换。时长单位可选秒/分钟，默认 5 分钟。
6. 手动切换 API **仅填充模式**有效：整池换槽，旧隧道排空后关。
7. 动态开启后停掉 tun0 单出口和「故障切最优」；故障只补热池槽位。关闭后恢复现状。
8. 换槽时旧槽上还有别的连接：立刻停接新连接，在途排空后再关（但必须先预连成功，避免空窗）。
9. 补槽用 LRU，不整轮清空「已用」集合。
10. API：`POST /{secret_path}/api/v1/dynamic-proxy/rotate`，Cookie 登录 **或** `Authorization: Bearer <管理员密码>`。202 + Job。
11. 粒度：按客户端新 TCP 选隧道，不按 HTTP 请求切（HTTPS/SOCKS 看不见请求）。
12. grok2api：所有非粘滞 `proxyPool` 节点（Build/Web/Console）走 `freshTunnel`（`request.Close=true`）。粘滞账号仍复用。
13. 预热：有 1 条 ready 就接；0 条立刻失败，不排队。
14. 不新开 token：Bearer 复用管理员密码。
15. 探测继续占用 `tun2–99`；热池只用 `dyn0–dyn10`。

## 架构

```
客户端 → :9527（唯一入口，协议嗅探不变）
         → 每条新 TCP 向 HotPool 取一条 ready 且未 draining 的槽
         → SO_BINDTODEVICE 绑定该 tun 拨号

HotPool：N = min(分类 ready, 10)
每槽：独立 OpenVPN 进程 + tun(dynN) + 策略路由表(110+N)
换槽：先预连下一条（最多 11）→ 成功 → 旧槽停接 → 排空 → 关闭
```

动态关闭时拆光热池，恢复 `tun0` + 表 100 + 现有 AutoSwitch/Health。

## 配置（`runtime_settings` 增列）

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `dynamic_enabled` | bool | false | 动态代理总开关 |
| `dynamic_rule` | `rotate` \| `sticky` | `rotate` | 轮询 / 填充 |
| `sticky_duration_seconds` | int | 300 | 归一化后的秒数（30–86400） |
| `sticky_duration_unit` | `s` \| `m` | `m` | 后台展示/输入单位 |

分类不另建表，沿用：

- `routing_mode` / `force_country` / `routing_ip_type` / `favorite_node_ids`

PUT `/settings` 校验：

- `dynamic_rule` 仅 `rotate`|`sticky`
- `sticky_duration_unit` 仅 `s`|`m`
- 换算后秒数 ∈ [30, 86400]；`m` 时输入分钟再 ×60
- 开启动态不要求 `connection_enabled` 与现逻辑冲突：动态开时连接由热池接管，`connection_enabled=false` 仍拆掉出口（含热池）

## 组件

### HotPoolService（新）

内存态 + 设置驱动：

- `slots[]`：nodeID、tun、table、状态（connecting/ready/draining/dead）、inflight、lastUsedAt、exitIP
- `SelectSlot() (*Slot, error)`：只返回 ready 且非 draining；0 条 → error
- `OnConnClosed(slot)`：inflight--；轮询且分类>10 且该槽曾承载连接 → `replaceSlot(slot)`
- `replaceSlot(old)`：LRU 选不在当前槽内的节点 → 预连新槽 → 成功才把 old 标 draining
- `RotateAll(ctx)`：仅 sticky+enabled；每个槽 replace；已有 rotate 进行中 → 409
- `Reconcile(settings)`：开关/分类变化时增删槽
- `Stop()`：全部停接、排空、拆隧道

选节点：`ApplyFilters` + status=ready + 不在黑名单 + 不在当前槽。并列时按 LRU（从未用过的优先，其次 `lastUsedAt` 最旧），再随机打散同分。

### Tunnel 多实例

现 `tunnel.Manager` 只养一条 `tun0`。热池不走它的 `Connect`（那会先 `disconnectLocked`）。

新增 `tunnel.Instance`（或 Manager 上的 `StartInstance(device, configText)`）：

- 独立 ovpn 配置文件、独立 `--dev dynN`
- `RouteNoPull: true`（与现网一致）
- 退出回调通知 HotPool 补槽

设备名：`dyn0`…`dyn9`，替换时临时 `dyn10`。

### PolicyRouter 多表

现实现 `Cleanup` 会拆掉整张配置表，不能多实例共用一个 Router。

每槽一个 `PolicyRouter`：

- table = `110 + index`（110–120）
- `ip route add default dev dynN table T`
- `ip rule add oif dynN table T`
- rp_filter 仍按现逻辑放宽；Cleanup 只删自己的 table/rule

动态开时**不**调用网关对 tun0/表 100 的 Setup。

### 本地代理

`OutboundConnector` 从「固定 iface」改为：

```
Dial(ctx, host, port)
  slot, err := pool.SelectSlot()
  if err != nil { return err }
  slot.Inflight++
  defer pool.OnConnClosed(slot)
  用 bindControl(slot.Iface) + 该槽 DNS 拨号
```

`:9527` 监听、鉴权、外网开关、连接上限不变。不解析 TLS，不按 HTTP 请求切隧道。

### 与现有单出口的互斥

| 动态开 | 动态关 |
|---|---|
| Gateway.Activate(tun0) 不跑 | 现逻辑不变 |
| AutoSwitch.Switch / HandleUnexpectedExit 空操作或转 HotPool.ReplaceDead | 现逻辑 |
| Health 恢复不切最优，只对挂掉的槽补 | 现逻辑 |
| Maintenance 连出口改为「热池至少 1 条 ready」 | 现逻辑 |
| 手动「激活某节点」：动态开时拒绝或改成「把该节点加入热池并替换最旧槽」（YAGNI：本轮拒绝，400） | 现逻辑 |

### API

```
GET  /api/v1/settings                  # 含新字段
PUT  /api/v1/settings                  # 含新字段；改动态开关会 Reconcile
GET  /api/v1/gateway/status            # 动态开时附加 hotpool 快照
POST /api/v1/dynamic-proxy/rotate      # 仅 sticky+enabled → 202 Job
GET  /api/v1/jobs/{id}                 # 沿用
```

鉴权（SecretPath 中间件扩展）：

- 有效 session Cookie，或
- `Authorization: Bearer <管理员明文密码>`（`Auth.Verify(username, password)`，username 取当前管理员用户名）
- 错：401
- 仍必须带 `/{secret_path}` 前缀

rotate Job 名：`rotate-hotpool`  
result：

```json
{
  "replaced": 8,
  "failed": 2,
  "slots": [
    {"node_id": "...", "exit_ip": "...", "ok": true, "error": null}
  ]
}
```

部分失败仍 Job succeeded；`failed>0` 留在 result。

轮询模式 / 动态关：400。已有 rotate：409。

### 后台

- 策略页：动态开关、规则（轮询/填充）、填充时长 + 单位（秒/分钟）
- 网关页：动态开时展示槽列表（节点、出口 IP、inflight、状态），隐藏单出口「切换节点」或改为调用 rotate（仅填充可用）

### grok2api（`/root/Grok/grok2api`）

改 `Lease.doRequest`：

```go
if l.freshTunnel { // 去掉 Scope==Build 限制
    request.Close = true
}
```

浏览器栈：`toFHTTPRequest` 已抄 `request.Close`。freshTunnel 的 browser 请求结束后可 `CloseIdleConnections()`，避免 tls-client 池住旧 CONNECT。

测试：

- 删除「Web proxy pool 必须复用」断言
- Web/Console + freshTunnel → 必须 Close
- 粘滞（freshTunnel=false）仍不 Close

指向 free-proxy 的出口节点仍需管理员勾 `proxyPool`（不按 URL 自动识别）。

## 错误处理

| 场景 | 行为 |
|---|---|
| 分类 ready=0 | 热池空，Dial 失败；后台提示无可用节点 |
| 预热中 0 ready | 立刻失败，不排队 |
| 槽 Connect 失败 | 节点按现有规则拉黑/退避；换下一个 LRU |
| 运行中 OpenVPN 挂 | 停接 → 在途失败/排空 → LRU 补。不切最优 |
| 轮询调 rotate | 400 `rotate is only available in sticky mode` |
| 动态关调 rotate | 400 `dynamic proxy is disabled` |
| Bearer/Cookie 无效 | 401 |
| rotate 进行中再调 | 409 |
| 预连失败 | 旧槽继续接新连接，稍后重试 |
| 探测 vs 热池 TUN | 探测 tun2–99，热池 dyn0–dyn10 |
| 连不满 10 条 | 有几条用几条 |
| 保存后分类变化 | 非法槽：先预连合法节点，再排空 |
| 关动态 | 停接 → 排空 → 拆光 → 恢复 tun0 |
| grok2api 粘滞账号 | 不设 Close |

## 测试策略

不真起 OpenVPN。隧道/路由/拨号用假 Runner、假 Connector。

free-proxy：

1. `N=min(ready,10)` + ApplyFilters
2. LRU 补槽：避开当前槽内节点，挑最久未用
3. 轮询换槽：预连成功才 draining；失败则旧槽仍 ready
4. 填充窗口内不 replace；到期触发；`m`/`s` 换算
5. rotate 仅 sticky+enabled；否则 400；进行中 409
6. Cookie 或 Bearer 管理员密码；错误 401
7. SelectSlot：0 ready → err；只返回非 draining
8. 动态开不 Activate tun0；关则 Stop 热池

grok2api：见上一节。

不做：真 VPNGate 连 10 条（手测）；不改探测并发默认。

## 非目标

- 按 HTTP 请求（明文 keep-alive 内）切隧道
- 独立 rotate token / 新端口 / 绕过 secret path
- 动态开启时保留 tun0「主出口」给网关面板
- 按代理 URL 自动识别 free-proxy
- 多机热池 / 跨进程共享槽

## 风险

- 弱鸡（1C/1G）同时 10 条 OpenVPN 可能打满 CPU：允许连不满，后台展示实际槽数。
- VPNGate 节点不稳定：失败即 LRU 换，不阻塞入口。
- 分类抖动导致频繁换槽：分类变化只替换「不再合法」的槽，合法槽保留。
