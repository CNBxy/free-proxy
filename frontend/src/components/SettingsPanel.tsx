import { useEffect, useRef, useState } from "react";
import * as api from "../api";
import type { PolicyMode, PriorityMetric, PriorityOrder, ProxySettings, QualityBracket, RoutingIpType } from "../types";
import { useUI } from "../store";
import { Card, Spinner, Toggle } from "./ui";
import { SystemConfigPanel } from "./SystemConfigPanel";

const METRIC_LABELS: Record<PriorityMetric, string> = {
  sessions: "节点连接的会话数最少",
  latency: "延迟",
  ping: "来源 Ping",
  speed: "来源速度",
};

const METRIC_UNITS: Record<PriorityMetric, string> = {
  sessions: "个会话",
  latency: "ms",
  ping: "ms",
  speed: "Mbps",
};

function CountryFilterDropdown({ available, selected, onChange }: {
  available: { code: string; country: string; country_zh: string; country_flag: string; ready: number }[];
  selected: string[];
  onChange: (codes: string[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, []);

  const toggle = (code: string) => {
    if (selected.includes(code)) {
      onChange(selected.filter((c) => c !== code));
    } else {
      onChange([...selected, code]);
    }
  };

  const allSelected = available.length > 0 && available.every((c) => selected.includes(c.code));

  return (
    <div ref={ref} className="relative">
      <label className="block">
        <span className="text-sm text-ink-2">国家筛选</span>
        <button
          type="button"
          className="field mt-1 w-full text-left flex items-center justify-between"
          onClick={() => setOpen(!open)}
        >
          <span className="truncate">
            {selected.length === 0
              ? "全部国家"
              : `已选 ${selected.length} / ${available.length} 个国家`}
          </span>
          <svg className={`w-4 h-4 ml-2 transition-transform ${open ? "rotate-180" : ""}`} fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
          </svg>
        </button>
      </label>
      {open && (
        <div className="absolute z-50 mt-1 w-full bg-white border border-rule rounded-md shadow-lg max-h-60 overflow-auto">
          <div className="sticky top-0 bg-white border-b border-rule px-3 py-2 flex items-center gap-2">
            <button
              type="button"
              className="text-xs text-primary hover:underline"
              onClick={() => onChange(available.map((c) => c.code))}
            >
              全选
            </button>
            <button
              type="button"
              className="text-xs text-primary hover:underline"
              onClick={() => onChange([])}
            >
              清除
            </button>
            <span className="text-xs text-ink-3 ml-auto">
              {selected.length}/{available.length}
            </span>
          </div>
          {available.map((c) => (
            <label
              key={c.code}
              className="flex items-center gap-2 px-3 py-1.5 hover:bg-surface cursor-pointer"
            >
              <input
                type="checkbox"
                className="w-4 h-4 rounded border-rule text-primary focus:ring-primary"
                checked={selected.includes(c.code)}
                onChange={() => toggle(c.code)}
              />
              <span className="text-sm">{c.country_flag} {c.country_zh || c.country}</span>
              <span className="text-xs text-ink-3 ml-auto">({c.ready})</span>
            </label>
          ))}
        </div>
      )}
    </div>
  );
}

function QualityBracketEditor({ metric, brackets, onChange }: {
  metric: PriorityMetric;
  brackets: QualityBracket[];
  onChange: (brackets: QualityBracket[]) => void;
}) {
  const unit = METRIC_UNITS[metric];
  const isSpeed = metric === "speed";

  const updateBracket = (index: number, field: keyof QualityBracket, value: number) => {
    const newBrackets = [...brackets];
    newBrackets[index] = { ...newBrackets[index], [field]: value };
    onChange(newBrackets);
  };

  const addBracket = () => {
    const lastMax = brackets.length > 0 ? brackets[brackets.length - 1].max : 0;
    onChange([...brackets, { min: lastMax + 1, max: lastMax + 100, score: 0 }]);
  };

  const removeBracket = (index: number) => {
    onChange(brackets.filter((_, i) => i !== index));
  };

  return (
    <div className="space-y-2">
      <div className="text-xs text-ink-3 font-medium">质量分配置 ({unit})</div>
      {brackets.map((b, i) => (
        <div key={i} className="flex items-center gap-2 text-xs">
          <span className="text-ink-3 w-8 text-right">{isSpeed ? "≥" : ""}{b.min}</span>
          <span className="text-ink-3">~</span>
          <span className="text-ink-3 w-8">{isSpeed ? "∞" : b.max}</span>
          <span className="text-ink-3">→</span>
          <input
            type="number"
            className="w-16 px-1.5 py-0.5 border border-rule rounded text-xs"
            value={b.score}
            onChange={(e) => updateBracket(i, "score", Number(e.target.value))}
          />
          <button
            type="button"
            className="text-ink-3 hover:text-danger"
            onClick={() => removeBracket(i)}
          >
            ×
          </button>
        </div>
      ))}
      <button
        type="button"
        className="text-xs text-primary hover:underline"
        onClick={addBracket}
      >
        + 添加区间
      </button>
    </div>
  );
}

function PriorityOrderEditor({ priorities, onChange }: {
  priorities: PriorityOrder[];
  onChange: (priorities: PriorityOrder[]) => void;
}) {
  const [expandedIndex, setExpandedIndex] = useState<number | null>(null);

  const updatePriority = (index: number, patch: Partial<PriorityOrder>) => {
    const newPriorities = [...priorities];
    newPriorities[index] = { ...newPriorities[index], ...patch };
    onChange(newPriorities);
  };

  const addPriority = () => {
    onChange([...priorities, { metric: "sessions", weight: 0.1, quality_ms: [] }]);
  };

  const removePriority = (index: number) => {
    onChange(priorities.filter((_, i) => i !== index));
  };

  const movePriority = (index: number, direction: -1 | 1) => {
    const newIndex = index + direction;
    if (newIndex < 0 || newIndex >= priorities.length) return;
    const newPriorities = [...priorities];
    [newPriorities[index], newPriorities[newIndex]] = [newPriorities[newIndex], newPriorities[index]];
    onChange(newPriorities);
  };

  const totalWeight = priorities.reduce((sum, p) => sum + p.weight, 0);

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-xs text-ink-3">
          优先顺排序（权重合计: {totalWeight.toFixed(2)}）
        </span>
        <button
          type="button"
          className="text-xs text-primary hover:underline"
          onClick={addPriority}
        >
          + 添加优先顺
        </button>
      </div>
      {priorities.map((p, i) => (
        <div key={i} className="border border-rule rounded-md p-2">
          <div className="flex items-center gap-2">
            <button
              type="button"
              className="text-ink-3 hover:text-ink"
              onClick={() => movePriority(i, -1)}
              disabled={i === 0}
            >
              ↑
            </button>
            <button
              type="button"
              className="text-ink-3 hover:text-ink"
              onClick={() => movePriority(i, 1)}
              disabled={i === priorities.length - 1}
            >
              ↓
            </button>
            <span className="text-xs font-medium">第{i + 1}</span>
            <select
              className="flex-1 text-xs px-1.5 py-0.5 border border-rule rounded"
              value={p.metric}
              onChange={(e) => updatePriority(i, { metric: e.target.value as PriorityMetric })}
            >
              {Object.entries(METRIC_LABELS).map(([key, label]) => (
                <option key={key} value={key}>{label}</option>
              ))}
            </select>
            <span className="text-xs text-ink-3">权重</span>
            <input
              type="number"
              className="w-16 px-1.5 py-0.5 border border-rule rounded text-xs"
              value={p.weight}
              step={0.05}
              min={0}
              max={1}
              onChange={(e) => updatePriority(i, { weight: Number(e.target.value) })}
            />
            <button
              type="button"
              className="text-ink-3 hover:text-danger"
              onClick={() => removePriority(i)}
            >
              ×
            </button>
          </div>
          <div className="mt-2 ml-8">
            <button
              type="button"
              className="text-xs text-ink-3 hover:text-ink"
              onClick={() => setExpandedIndex(expandedIndex === i ? null : i)}
            >
              {expandedIndex === i ? "收起配置" : "展开配置"}
            </button>
            {expandedIndex === i && (
              <div className="mt-2">
                <QualityBracketEditor
                  metric={p.metric}
                  brackets={p.quality_ms}
                  onChange={(brackets) => updatePriority(i, { quality_ms: brackets })}
                />
              </div>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

export function SettingsPanel({ settings, onChanged }: { settings: ProxySettings | null; onChanged: () => void }) {
  const push = useUI((s) => s.push);
  const [form, setForm] = useState<ProxySettings | null>(settings);
  const [busy, setBusy] = useState(false);
  const [availableCountries, setAvailableCountries] = useState<{ code: string; country: string; country_zh: string; country_flag: string; ready: number }[]>([]);
  const dirty = useRef(false);

  useEffect(() => {
    if (!settings) return;
    setForm((current) => {
      if (!dirty.current || !current) return settings;
      return { ...current, favorite_node_ids: settings.favorite_node_ids };
    });
  }, [settings]);

  useEffect(() => {
    api.listProxyCountries({ status: "ready" }).then(({ items }) => {
      setAvailableCountries(items);
    }).catch(() => {});
  }, []);

  if (!form) return null;

  const set = (patch: Partial<ProxySettings>) => {
    dirty.current = true;
    setForm((current) => current ? { ...current, ...patch } : current);
  };

  async function save() {
    if (!form) return;
    setBusy(true);
    try {
      const saved = await api.updateSettings({
        routing_mode: form.routing_mode,
        force_country: form.force_country,
        routing_ip_type: form.routing_ip_type,
        connection_enabled: form.connection_enabled,
        fixed_node_id: form.fixed_node_id,
        country_filters: form.country_filters,
        priority_order: form.priority_order,
      });
      dirty.current = false;
      setForm(saved);
      push("ok", "策略已保存");
      onChanged();
    } catch (e) {
      push("error", (e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="grid gap-4">
      <Card
        title="路由策略"
        actions={<button className="btn btn-primary" disabled={busy} onClick={save}>{busy ? <Spinner /> : "保存"}</button>}
      >
        <div className="grid sm:grid-cols-2 gap-4">
          <label className="block">
            <span className="text-sm text-ink-2">路由模式</span>
            <select className="field mt-1" value={form.routing_mode}
              onChange={(e) => set({ routing_mode: e.target.value as PolicyMode })}>
              <option value="auto">延迟优先</option>
              <option value="speed_first">速度优先</option>
              <option value="smart">智能（综合）</option>
              <option value="residential_first">住宅优先</option>
              <option value="country">指定国家</option>
              <option value="fixed">固定节点</option>
              <option value="favorites">仅收藏</option>
            </select>
          </label>
          <label className="block">
            <span className="text-sm text-ink-2">IP 类型</span>
            <select className="field mt-1" value={form.routing_ip_type}
              onChange={(e) => set({ routing_ip_type: e.target.value as RoutingIpType })}>
              <option value="all">全部</option>
              <option value="residential">住宅 / 移动</option>
              <option value="hosting">机房</option>
            </select>
          </label>
          {form.routing_mode === "country" && (
            <label className="block">
              <span className="text-sm text-ink-2">国家（中文名、英文名或代码）</span>
              <input className="field mt-1" value={form.force_country}
                onChange={(e) => set({ force_country: e.target.value })} placeholder="例如 日本、Japan 或 JP" />
            </label>
          )}
          {form.routing_mode === "fixed" && (
            <label className="block">
              <span className="text-sm text-ink-2">固定节点 ID</span>
              <input className="field mt-1" value={form.fixed_node_id ?? ""}
                onChange={(e) => set({ fixed_node_id: e.target.value })} placeholder="节点 ID" />
            </label>
          )}
          <CountryFilterDropdown
            available={availableCountries}
            selected={form.country_filters}
            onChange={(codes) => set({ country_filters: codes })}
          />
          <Toggle label="启用自动连接出口" hint="关闭后不会自动挑选并连接出口"
            checked={form.connection_enabled}
            onChange={(v) => set({ connection_enabled: v })} />
        </div>
        <p className="text-xs text-ink-3 mt-4">
          延迟优先选择响应最快的节点；速度优先选择来源标注带宽最高的节点；智能策略综合延迟（40%）、速度（40%）和 VPN Gate 会话数（20%，越少越好）。手动切换节点后会自动锁定为固定节点，避免后台自动切回其他节点。
          收藏节点数：{form.favorite_node_ids.length}。修改策略后系统会自动校验当前出口是否仍符合规则。
        </p>
      </Card>

      <Card title="优先顺排序">
        <p className="text-xs text-ink-3 mb-3">
          配置节点选择的优先顺和权重。系统会在每次轮换时根据权重 × 质量分的合计值选择得分最高的节点。
        </p>
        <PriorityOrderEditor
          priorities={form.priority_order}
          onChange={(priorities) => set({ priority_order: priorities })}
        />
      </Card>

      <SystemConfigPanel />
    </div>
  );
}
