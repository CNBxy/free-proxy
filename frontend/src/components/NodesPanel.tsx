import { useCallback, useEffect, useRef, useState } from "react";
import * as api from "../api";
import type { CountryFacet, ProxyNode, ProxySettings } from "../types";
import { useUI } from "../store";
import { Badge, Card, Spinner } from "./ui";

const PAGE = 20;
// A keystroke is a poor trigger for a round trip: unthrottled, a five-letter
// query costs five list requests whose answers can land out of order.
const SEARCH_DEBOUNCE_MS = 300;

export function NodesPanel({ settings, onChanged, favoriteOnly = false }: {
  settings: ProxySettings | null;
  onChanged: () => void;
  favoriteOnly?: boolean;
}) {
  const push = useUI((s) => s.push);
  const [items, setItems] = useState<ProxyNode[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [country, setCountry] = useState("");
  const [countries, setCountries] = useState<CountryFacet[]>([]);
  const [ipType, setIpType] = useState("");
  const [status, setStatus] = useState("");
  const [listedOnly, setListedOnly] = useState(false);
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState("");
  // Bumped whenever a job changes the pool, so the country picker recounts.
  const [poolVersion, setPoolVersion] = useState(0);
  const requestSeq = useRef(0);

  const favorites = new Set(settings?.favorite_node_ids ?? []);

  useEffect(() => {
    const next = searchInput.trim();
    if (next === search) return;
    const timer = setTimeout(() => {
      setPage(0);
      setSearch(next);
    }, SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [searchInput, search]);

  const load = useCallback(async () => {
    const seq = ++requestSeq.current;
    setLoading(true);
    try {
      const res = await api.listProxies({
        limit: PAGE, offset: page * PAGE, search, country, ip_type: ipType, status,
        favorite: favoriteOnly,
        listed_only: !favoriteOnly && listedOnly,
      });
      // Typing leaves several requests in flight even with the debounce; only
      // the newest may write to the table.
      if (seq !== requestSeq.current) return;
      setItems(res.items);
      setTotal(res.total);
    } catch (e) {
      if (seq === requestSeq.current) push("error", (e as Error).message);
    } finally {
      if (seq === requestSeq.current) setLoading(false);
    }
  }, [page, search, country, ipType, status, listedOnly, favoriteOnly, push]);

  useEffect(() => {
    load();
  }, [load]);

  // The country picker follows every filter except the country itself, so
  // picking one does not empty the list it was picked from.
  useEffect(() => {
    let live = true;
    api.listProxyCountries({
      ip_type: ipType, status, favorite: favoriteOnly, listed_only: !favoriteOnly && listedOnly,
    })
      .then((res) => { if (live) setCountries(res.items.filter((c) => c.code !== "")); })
      .catch(() => { /* the table still works without the picker */ });
    return () => { live = false; };
  }, [ipType, status, listedOnly, favoriteOnly, poolVersion]);

  async function runJob(label: string, fn: () => Promise<{ id: string }>) {
    setBusy(label);
    try {
      const job = await fn();
      await api.waitJob(job.id);
      push("ok", `${label}完成`);
      setPoolVersion((v) => v + 1);
      await load();
      onChanged();
    } catch (e) {
      push("error", `${label}失败：${(e as Error).message}`);
    } finally {
      setBusy("");
    }
  }

  const filtered = !!(search || country || ipType || status || listedOnly);

  function resetFilters() {
    setPage(0);
    setSearchInput("");
    setSearch("");
    setCountry("");
    setIpType("");
    setStatus("");
    setListedOnly(false);
  }

  async function favorite(id: string) {
    try {
      const updated = await api.toggleFavorite(id);
      const kept = updated.favorite_node_ids.includes(id);
      push("ok", kept ? "已加入收藏" : "已取消收藏");
      if (!kept) {
        setSelected((prev) => {
          const next = new Set(prev);
          next.delete(id);
          return next;
        });
      }
      onChanged();
      await load();
    } catch (e) {
      push("error", (e as Error).message);
    }
  }

  const toggleSel = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });

  const pages = Math.max(1, Math.ceil(total / PAGE));

  useEffect(() => {
    if (page >= pages && page > 0) setPage(pages - 1);
  }, [page, pages]);

  return (
    <Card
      title={`${favoriteOnly ? "收藏节点" : listedOnly ? "来源最新名单" : "当前节点池"}（${total}）`}
      actions={
        <>
          {!favoriteOnly && <>
            <button className="btn btn-primary" disabled={!!busy}
              onClick={() => runJob("更新并检测", api.refresh)}>
              {busy === "更新并检测" ? <Spinner /> : "更新并检测节点"}
            </button>
            <button className="btn" disabled={!!busy} onClick={() => runJob("发现节点", api.discover)}>
              {busy === "发现节点" ? <Spinner /> : "仅发现"}
            </button>
          </>}
          <button className="btn" disabled={!!busy || selected.size === 0}
            onClick={() => runJob("测试节点", () => api.probeMany([...selected]))}
            title="测试节点是否能连接，并记录实际延迟">
            测试节点（{selected.size}）
          </button>
        </>
      }
    >
      <div className="flex flex-wrap gap-2 mb-4">
        <div className="relative flex-1 min-w-[220px]">
          <input className="field pr-8" placeholder="搜索 国家 / 城市 / IP / 机构 / ASN（空格分隔多个条件）"
            value={searchInput} onChange={(e) => setSearchInput(e.target.value)} />
          {searchInput && (
            <button type="button" aria-label="清空搜索" title="清空搜索"
              className="absolute right-2.5 top-1/2 -translate-y-1/2 text-ink-3 hover:text-ink text-sm leading-none"
              onClick={() => setSearchInput("")}>✕</button>
          )}
        </div>
        <select className="field w-auto max-w-[210px]" value={country}
          onChange={(e) => { setPage(0); setCountry(e.target.value); }}>
          <option value="">全部国家/地区</option>
          {country && !countries.some((c) => c.code === country) && (
            <option value={country}>{country}</option>
          )}
          {countries.map((c) => (
            <option key={c.code} value={c.code}>
              {`${c.country_flag || "🏳"} ${c.country_zh || c.country || c.code}（${c.total}）`}
            </option>
          ))}
        </select>
        <select className="field w-auto" value={ipType} onChange={(e) => { setPage(0); setIpType(e.target.value); }}>
          <option value="">全部类型</option>
          <option value="residential">住宅</option>
          <option value="mobile">移动</option>
          <option value="hosting">机房</option>
          <option value="unknown">未知</option>
        </select>
        <select className="field w-auto" value={status} onChange={(e) => { setPage(0); setStatus(e.target.value); }}>
          <option value="">全部状态</option>
          <option value="ready">可用</option>
          <option value="discovered">已发现</option>
          <option value="unavailable">不可用</option>
          <option value="cooldown">冷却</option>
        </select>
        {!favoriteOnly && <label className="flex items-center gap-2 px-2 text-sm text-ink-2 whitespace-nowrap">
          <input type="checkbox" checked={listedOnly}
            onChange={(e) => { setPage(0); setListedOnly(e.target.checked); }} />
          仅来源最新名单
        </label>}
        {filtered && <button className="btn" onClick={resetFilters}>清除筛选</button>}
        <button className="btn" onClick={load} disabled={loading}>{loading ? <Spinner /> : "刷新"}</button>
      </div>

      <p className="text-xs text-ink-3 mb-3">
        {favoriteOnly ? <>
          这里包含全部收藏记录；标记为“未在名单”的节点只是不在来源最近一次公布的名单里，多数仍然可用，
          可以先测试确认后再切换。取消收藏后节点会从本页移除。
        </> : <>
          默认显示节点池全部节点，每页 {PAGE} 条。VPN Gate 每次只公布约 100 台轮换节点，节点池会持续累积，
          并由后台探活自动删除确认失效的节点，因此“未在名单”不代表不可用。
        </>}
        “切换节点”会立即使用该节点，并自动改为固定节点；“测试节点”只检查连接和延迟，不会切换当前节点。
        搜索支持中文国家名、城市、机构、ASN 和 IP（例如“日本 东京”“韩国 SK”），多个关键词用空格分隔，需同时满足。
      </p>
      <div className="overflow-x-auto rounded-md border border-rule">
        <table className="w-full min-w-[980px] border-collapse">
          <thead>
            <tr>
              <th className="th w-8"></th>
              <th className="th">国家 / 地区 / 机构</th>
              <th className="th">IP</th>
              <th className="th">类型</th>
              <th className="th">状态</th>
              <th className="th">延迟</th>
              <th className="th">来源 Ping</th>
              <th className="th">来源速度</th>
              <th className="th" title="VPN Gate 最近一次抓取时的会话数，越少表示节点负载越低">会话数</th>
              <th className="th text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            {items.length === 0 && (
              <tr><td className="td text-center text-ink-3 py-8" colSpan={10}>
                {loading ? "加载中…"
                  : filtered ? "没有符合当前条件的节点，可换个关键词或点击“清除筛选”。"
                  : favoriteOnly ? "暂无收藏节点，请先在节点页面收藏常用节点。"
                  : "暂无节点，点击“更新并检测节点”开始。"}
              </td></tr>
            )}
            {items.map((n) => (
              <tr key={n.id} className="hover:bg-paper-2/50">
                <td className="td">
                  <input type="checkbox"
                    checked={selected.has(n.id)} onChange={() => toggleSel(n.id)} />
                </td>
                <td className="td">
                  <div className="font-medium whitespace-nowrap" title={n.country || n.country_code || ""}>
                    <span className="mr-1.5">{n.country_flag || "🏳"}</span>
                    {n.country_zh || n.country || n.country_code || "—"}
                  </div>
                  <div className="text-xs text-ink-3 truncate max-w-[260px]" title={detailOf(n)}>
                    {detailOf(n) || "—"}
                  </div>
                </td>
                <td className="td font-mono text-[0.8rem]">{n.ip_address}<div className="text-xs text-ink-3 font-sans">{n.transport}</div></td>
                <td className="td"><Badge label={ipLabel(n.ip_type)} tone={n.ip_type} /></td>
                <td className="td">
                  <Badge label={statusLabel(n.status)} tone={n.status} />
                  {!n.source_present && <div className="text-xs text-ink-3 mt-1"
                    title="不在来源最近一次公布的名单里；来源每次只公布约 100 台轮换节点，这不代表节点不可用">未在名单</div>}
                </td>
                <td className="td tabular-nums">{n.latency_ms > 0 ? `${n.latency_ms} ms` : "—"}</td>
                <td className="td tabular-nums text-ink-3">{n.source_ping_ms > 0 ? `${n.source_ping_ms} ms` : "—"}</td>
                <td className="td tabular-nums text-ink-3">{formatSpeed(n.source_speed_bps)}</td>
                <td className="td tabular-nums text-ink-3">{n.source_sessions}</td>
                <td className="td text-right whitespace-nowrap">
                  <button className="btn btn-sm btn-primary mr-1" disabled={!!busy}
                    onClick={() => runJob("切换节点", () => api.activate(n.id))}
                    title="切换到此节点，并锁定为固定节点">
                    切换节点
                  </button>
                  <button className="btn btn-sm mr-1" disabled={!!busy}
                    onClick={() => runJob("测试节点", () => api.probeOne(n.id))}
                    title="测试此节点是否能连接，并记录实际延迟">
                    测试节点
                  </button>
                  <button className="btn btn-sm mr-1" onClick={() => favorite(n.id)}>
                    {favoriteOnly ? "取消收藏" : favorites.has(n.id) ? "★" : "☆"}
                  </button>
                  <a className="btn btn-sm" href={api.configUrl(n.id)}>下载</a>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="flex items-center justify-between mt-4 text-sm text-ink-3">
        <span>
          第 {page + 1} / {pages} 页
          {total > 0 && ` · 当前显示 ${page * PAGE + 1}–${Math.min((page + 1) * PAGE, total)}，共 ${total} 个`}
        </span>
        <div className="flex gap-2">
          <button className="btn btn-sm" disabled={page === 0} onClick={() => setPage((p) => p - 1)}>上一页</button>
          <button className="btn btn-sm" disabled={page + 1 >= pages} onClick={() => setPage((p) => p + 1)}>下一页</button>
        </div>
      </div>
    </Card>
  );
}

// The enrichment API is asked for Chinese, so location reads "日本 东京都 涩谷区".
// The country already leads the cell, so only what follows it is worth
// repeating — with the operator after it, since a node is identified as much by
// who runs it as by where it is.
function detailOf(n: ProxyNode) {
  let address = (n.location || "").trim();
  for (const prefix of [n.country_zh, n.country]) {
    if (prefix && address.startsWith(prefix)) {
      address = address.slice(prefix.length).trim();
      break;
    }
  }
  return [address, n.owner || n.as_name || n.host_name].filter(Boolean).join(" · ");
}

function ipLabel(t: string) {
  return { residential: "住宅", mobile: "移动", hosting: "机房", unknown: "未知" }[t] ?? t;
}
function statusLabel(s: string) {
  return { ready: "可用", discovered: "已发现", probing: "测试中", unavailable: "不可用", cooldown: "冷却" }[s] ?? s;
}

function formatSpeed(bps: number) {
  if (!bps || bps <= 0) return "—";
  if (bps >= 1_000_000) return `${(bps / 1_000_000).toFixed(1)} Mbps`;
  if (bps >= 1_000) return `${Math.round(bps / 1_000)} Kbps`;
  return `${bps} bps`;
}
