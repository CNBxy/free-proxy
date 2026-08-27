import { useEffect, useState } from "react";
import * as api from "../api";
import type { AppSettings } from "../types";
import { useUI } from "../store";
import { Card, Spinner } from "./ui";

type Section = keyof AppSettings;

export function SystemConfigPanel() {
  const push = useUI((s) => s.push);
  const [form, setForm] = useState<AppSettings | null>(null);
  const [adminPassword, setAdminPassword] = useState("");
  const [proxyPassword, setProxyPassword] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => { api.getSystemConfig().then(setForm).catch((e) => push("error", (e as Error).message)); }, [push]);
  if (!form) return <Card title="系统配置"><div className="text-sm text-ink-3">加载中…</div></Card>;

  const set = <S extends Section>(section: S, key: keyof AppSettings[S], value: unknown) =>
    setForm({ ...form, [section]: { ...form[section], [key]: value } });
  const num = <S extends Section>(section: S, key: keyof AppSettings[S]) => (e: React.ChangeEvent<HTMLInputElement>) =>
    set(section, key, Number(e.target.value));

  async function save() {
    setBusy(true);
    try {
      const result = await api.updateSystemConfig(form!, adminPassword, proxyPassword);
      setForm(result.settings);
      setAdminPassword(""); setProxyPassword("");
      push("ok", "系统配置已保存，服务将在 2 秒后重启");
    } catch (e) { push("error", (e as Error).message); }
    finally { setBusy(false); }
  }

  return <div className="grid gap-4">
    <Card title="后台与代理服务" actions={<button className="btn btn-primary" disabled={busy} onClick={save}>{busy ? <Spinner /> : "保存配置"}</button>}>
      <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <Field label="后台用户名"><input className="field mt-1" value={form.admin.username} onChange={(e) => set("admin", "username", e.target.value)} /></Field>
        <Field label={`后台新密码${form.admin.password_set ? "（留空保持）" : ""}`}><input type="password" className="field mt-1" value={adminPassword} onChange={(e) => setAdminPassword(e.target.value)} /></Field>
        <Field label="管理路径"><input className="field mt-1" value={form.admin.secret_path} onChange={(e) => set("admin", "secret_path", e.target.value)} /></Field>
        <Field label="网页端口"><input type="number" className="field mt-1" value={form.admin.web_port} onChange={num("admin", "web_port")} /></Field>
        <Check label="允许网页后台外网访问" checked={form.admin.web_external_access} onChange={(v) => set("admin", "web_external_access", v)} />
        <Field label="代理用户名"><input className="field mt-1" value={form.proxy.username} onChange={(e) => set("proxy", "username", e.target.value)} /></Field>
        <Field label={`代理新密码${form.proxy.password_set ? "（留空保持）" : ""}`}><input type="password" className="field mt-1" value={proxyPassword} onChange={(e) => setProxyPassword(e.target.value)} /></Field>
        <Field label="代理端口"><input type="number" className="field mt-1" value={form.proxy.port} onChange={num("proxy", "port")} /></Field>
        <Check label="启用代理服务" checked={form.proxy.enabled} onChange={(v) => set("proxy", "enabled", v)} />
        <Check label="允许代理端口外网访问" checked={form.proxy.external_access} onChange={(v) => set("proxy", "external_access", v)} />
      </div>
      <p className="text-xs text-ink-3 mt-4">
        监听地址固定为 <code>0.0.0.0</code>；外部代理访问需同时开启开关并配置代理用户名和密码。密码只保存 scrypt 哈希。
      </p>
      <p className="text-xs text-ink-3 mt-2">
        检测间隔、探测并发、各类超时、数据源地址等参数不再暴露为配置项——它们各自只有一个正确取值，已固定在程序内部。
      </p>
    </Card>
  </div>;
}

function Field({ label, wide, children }: { label: string; wide?: boolean; children: React.ReactNode }) {
  return <label className={`block ${wide ? "sm:col-span-2 lg:col-span-3" : ""}`}><span className="text-sm text-ink-2">{label}</span>{children}</label>;
}
function Check({ label, checked, onChange }: { label: string; checked: boolean; onChange: (v: boolean) => void }) {
  return <label className="flex items-center gap-3 self-end min-h-10"><input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} /><span className="text-sm text-ink-2">{label}</span></label>;
}
