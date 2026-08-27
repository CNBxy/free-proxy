import { useEffect, useState } from "react";
import * as api from "../api";
import type { AppSettings } from "../types";
import { useUI } from "../store";
import { Card, Spinner, Toggle } from "./ui";

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
      {/* Two groups, one column rhythm. The fields and the switches sit in
          separate grids with the same gaps, so a switch never lands mid-row
          between two inputs and every left edge lines up down the card. */}
      <div className="grid gap-7">
        <Group title="网页后台" hint="登录这个面板的身份与入口">
          <Fields>
            <Field label="后台用户名">
              <input className="field mt-1.5" autoComplete="off" value={form.admin.username}
                onChange={(e) => set("admin", "username", e.target.value)} />
            </Field>
            <Field label="后台新密码">
              <input type="password" className="field mt-1.5" autoComplete="new-password"
                placeholder={form.admin.password_set ? "留空保持当前密码" : "设置密码"}
                value={adminPassword} onChange={(e) => setAdminPassword(e.target.value)} />
            </Field>
            <Field label="管理路径">
              <input className="field mt-1.5" value={form.admin.secret_path}
                onChange={(e) => set("admin", "secret_path", e.target.value)} />
            </Field>
            <Field label="网页端口">
              <input type="number" inputMode="numeric" min={1} max={65535} className="field field-num mt-1.5"
                value={form.admin.web_port} onChange={num("admin", "web_port")} />
            </Field>
          </Fields>
          <Switches>
            <Toggle label="允许网页后台外网访问" hint="关闭后非本机请求一律返回 404"
              checked={form.admin.web_external_access}
              onChange={(v) => set("admin", "web_external_access", v)} />
          </Switches>
        </Group>

        <Group title="代理服务" hint="客户端连接代理时使用的凭据与端口">
          <Fields>
            <Field label="代理用户名">
              <input className="field mt-1.5" autoComplete="off" value={form.proxy.username}
                onChange={(e) => set("proxy", "username", e.target.value)} />
            </Field>
            <Field label="代理新密码">
              <input type="password" className="field mt-1.5" autoComplete="new-password"
                placeholder={form.proxy.password_set ? "留空保持当前密码" : "设置密码"}
                value={proxyPassword} onChange={(e) => setProxyPassword(e.target.value)} />
            </Field>
            <Field label="代理端口">
              <input type="number" inputMode="numeric" min={1} max={65535} className="field field-num mt-1.5"
                value={form.proxy.port} onChange={num("proxy", "port")} />
            </Field>
          </Fields>
          <Switches>
            <Toggle label="启用代理服务" hint="关闭并保存后不再监听代理端口"
              checked={form.proxy.enabled} onChange={(v) => set("proxy", "enabled", v)} />
            <Toggle label="允许代理端口外网访问" hint="须同时设置代理用户名和密码"
              checked={form.proxy.external_access} onChange={(v) => set("proxy", "external_access", v)} />
          </Switches>
        </Group>
      </div>

      <div className="mt-7 pt-4 border-t border-rule space-y-1.5 text-xs text-ink-3">
        <p>监听地址固定为 <code>0.0.0.0</code>；外网访问开关即时生效，其余修改保存后服务自动重启。密码只保存 scrypt 哈希。</p>
        <p>检测间隔、探测并发、各类超时、数据源地址等参数不再暴露为配置项——它们各自只有一个正确取值，已固定在程序内部。</p>
      </div>
    </Card>
  </div>;
}

function Group({ title, hint, children }: { title: string; hint: string; children: React.ReactNode }) {
  return <section>
    <header className="flex items-baseline gap-3 mb-3.5">
      <h3 className="text-sm font-medium text-ink whitespace-nowrap">{title}</h3>
      <span className="text-xs text-ink-3 truncate">{hint}</span>
      <span className="flex-1 h-px bg-rule" />
    </header>
    {children}
  </section>;
}

// Four columns at desktop width, two below it: every field in the card is the
// same width, so the group with three fields still lines up with the group
// with four.
function Fields({ children }: { children: React.ReactNode }) {
  return <div className="grid gap-x-5 gap-y-4 sm:grid-cols-2 lg:grid-cols-4">{children}</div>;
}

// Half-width rows on the same 20px gutter, so their edges fall on the field
// columns above rather than near them.
function Switches({ children }: { children: React.ReactNode }) {
  return <div className="mt-4 grid gap-x-5 gap-y-3 sm:grid-cols-2">{children}</div>;
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <label className="block"><span className="text-sm text-ink-2">{label}</span>{children}</label>;
}
