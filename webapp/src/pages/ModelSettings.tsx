import { useEffect, useState } from "react";
import { api } from "../api";
import { t } from "../i18n";

export type ModelMapping = { publicModel: string; upstreamTone: string; displayName?: string; defaultReasoningLevel?: string; accountScope?: string; disabled?: boolean; scenario?: string; routeKind?: string };
type Probe = { failureReason?: string; accountId: string; tone: string; scenario?: string; status: string; checkedAt: string; latencyMs: number };
const scopes = [["all", "All accounts"], ["personal-premium", "Personal Premium"], ["personal-free", "Personal Free"], ["organization", "Work or school account"]];
const blank = (): ModelMapping => ({ publicModel: "", upstreamTone: "", displayName: "", defaultReasoningLevel: "none", accountScope: "all", routeKind: "model" });

export function ModelSettings({ mappings, setMappings, hideBuiltins, setHideBuiltins, push }: {
 mappings: ModelMapping[]; setMappings: (m: ModelMapping[]) => void; hideBuiltins: boolean; setHideBuiltins: (v: boolean) => void;
 push: (m: string, k?: "success" | "error" | "info") => void;
}) {
 const [form, setForm] = useState<ModelMapping>(blank);
 const [editing, setEditing] = useState<number | null>(null);
 const [source, setSource] = useState("");
 const [discovery, setDiscovery] = useState<any>({});
 const [tones, setTones] = useState<string[]>([]);
 const [accounts, setAccounts] = useState<any[]>([]);
 const [accountId, setAccountId] = useState("");
 const [probes, setProbes] = useState<Probe[]>([]);
 const [busy, setBusy] = useState("");
 const [message, setMessage] = useState("");
 useEffect(() => { let cancelled = false;
  void Promise.all([api("/api/admin/models/discovery"), api("/api/accounts")]).then(([d,a]) => {
   if (cancelled) return; setDiscovery(d.discovery || {}); setTones(d.upstreamTones || []); setProbes(d.probes || []); setAccounts(a.accounts || []); setAccountId(a.accounts?.[0]?.id || "");
  }).catch((e) => { if (!cancelled) setMessage(String(e.message || e)); });
  return () => { cancelled = true; };
 }, []);
 const sync = async () => {
  setBusy("sync"); setMessage("");
  try { const d = await api("/api/admin/models/sync", { method: "POST", body: JSON.stringify({ source: source.trim() }) }); setDiscovery(d.discovery); setTones(d.upstream_tones || []); setMessage(t("Candidate discovery updated. Account access and model identity are not confirmed.")); }
  catch (e: any) { setMessage(String(e.message || e)); } finally { setBusy(""); }
 };
 const save = async () => {
  setBusy("save");
  try { await api("/api/admin/settings", { method: "PUT", body: JSON.stringify({ modelMappings: mappings, hideBuiltinModels: hideBuiltins }) }); push(t("Model settings saved"), "success"); }
  catch (e: any) { push(String(e.message || e), "error"); } finally { setBusy(""); }
 };
 const add = () => {
  const next = { ...form, publicModel: form.publicModel.trim(), upstreamTone: form.upstreamTone.trim(), displayName: form.displayName?.trim() || form.publicModel.trim() };
  if (!/^[A-Za-z0-9._-]{1,128}$/.test(next.publicModel) || !/^[A-Za-z0-9._-]{1,128}$/.test(next.upstreamTone)) { push(t("Enter valid public and upstream identifiers"), "error"); return; }
  if (mappings.some((m,i) => i !== editing && m.publicModel.toLowerCase() === next.publicModel.toLowerCase())) { push(t("Mapping already exists"), "error"); return; }
  setMappings(editing === null ? [...mappings, next] : mappings.map((m,i) => i === editing ? next : m)); setForm(blank()); setEditing(null);
 };
 const probe = async (tone: string, scenario = "") => {
  if (!accountId || !tone) return;
  setBusy("probe:" + tone); setMessage("");
  try { const d = await api("/api/admin/models/probe", { method: "POST", body: JSON.stringify({ accountId, tone, scenario }) }); const p: Probe = d.result;
   setProbes(prev => [...prev.filter(x => !(x.accountId === p.accountId && x.tone === p.tone && (x.scenario || "") === (p.scenario || ""))), p]); setMessage(p.failureReason || t(p.status === "request_ok_identity_unconfirmed" ? "Request succeeded; resolved model identity is unconfirmed." : "Request failed. This route is not confirmed available for the selected account."));
  } catch (e: any) { setMessage(String(e.message || e)); } finally { setBusy(""); }
 };
 return <div className="card">
  <div className="card-head"><span>{t("Model discovery and custom routes")}</span><button className="btn btn-sm primary" disabled={!!busy} onClick={() => void save()}>{t("Save model settings")}</button></div>
  <div className="card-body">
   <p>{t("Response modes choose how Copilot answers; their underlying model can change. A candidate identifier or successful reply does not prove exact model identity or account entitlement.")}</p>
   <label className="form-label" htmlFor="model-source">{t("Microsoft bundle URL (optional)")}</label>
   <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}><input id="model-source" className="form-input" style={{ flex: 1, minWidth: 260 }} value={source} onChange={e => setSource(e.target.value)} placeholder="https://res.public.onecdn.static.microsoft/midgard/versionless-v2/main.…js" /><button className="btn" disabled={!!busy} onClick={() => void sync()}>{busy === "sync" ? t("Discovering…") : t("Sync Microsoft candidates")}</button></div>
   <p style={{ fontSize: 12, overflowWrap: "anywhere" }}>{t("Discovery source")}: {discovery.source || "—"}{discovery.syncedAt && !String(discovery.syncedAt).startsWith("0001") ? " · " + new Date(discovery.syncedAt).toLocaleString() : ""}</p>
   {discovery.warning && <p className="highlight-box warning">{t(discovery.warning)}</p>}
   <details><summary>{t("Discovered candidates (not an account model list)")} ({discovery.candidates?.length || 0})</summary>
    <div style={{ display: "flex", gap: 6, flexWrap: "wrap", marginTop: 10 }}>{(discovery.candidates || []).map((tone: string) => <button key={tone} className="btn btn-sm" onClick={() => { setEditing(null); setForm({ ...blank(), publicModel: tone.toLowerCase().replaceAll("_", "-"), upstreamTone: tone, displayName: tone }); }}>{tone}</button>)}</div>
   </details>
   <label style={{ display: "flex", gap: 8, marginTop: 16 }}><input type="checkbox" checked={hideBuiltins} onChange={e => setHideBuiltins(e.target.checked)} />{t("Publish custom routes only (hide built-in catalog)")}</label>
   <p style={{ fontSize: 12 }}>{t("Unchecked routes are excluded from /v1/models and cannot be called. Save model settings to apply changes.")}</p>
  </div>
  <div className="table-wrap"><table className="table"><thead><tr><th>{t("Published")}</th><th>{t("Public model")}</th><th>{t("Upstream tone")}</th><th>{t("Route type")}</th><th>{t("Account scope")}</th><th>{t("Last request test")}</th><th></th></tr></thead>
   <tbody>{mappings.length === 0 ? <tr><td colSpan={7} className="empty">{t("No custom routes. Add a mode or a model candidate below.")}</td></tr> : mappings.map((m,i) => {
    const p = probes.find(x => x.accountId === accountId && x.tone === m.upstreamTone && (x.scenario || "") === (m.scenario || ""));
    return <tr key={m.publicModel}><td><input aria-label={t("Published") + ": " + m.publicModel} type="checkbox" checked={!m.disabled} onChange={e => setMappings(mappings.map((x,j) => i===j ? { ...x, disabled: !e.target.checked } : x))} /></td><td>{m.displayName || m.publicModel}<br /><small>{m.publicModel}</small></td><td>{m.upstreamTone}<br /><small>{m.scenario || t("Automatic by account")}</small></td><td>{t(m.routeKind === "mode" || m.upstreamTone.toLowerCase() === "magic" ? "Response mode" : "Model candidate")}</td><td>{t(scopes.find(x => x[0] === (m.accountScope || "all"))?.[1] || "All accounts")}</td><td>{p ? <>{t(p.status === "request_ok_identity_unconfirmed" ? "Request OK · identity unconfirmed" : "Request failed")}<br /><small>{new Date(p.checkedAt).toLocaleString()}</small></> : t("Not tested")}</td><td><button className="btn btn-sm" disabled={!!busy || !accountId} onClick={() => void probe(m.upstreamTone, m.scenario)}>{t("Test request")}</button>{" "}<button className="btn btn-sm" onClick={() => { setForm({ ...m }); setEditing(i); }}>{t("Edit")}</button>{" "}<button className="btn btn-sm danger" onClick={() => { setMappings(mappings.filter((_,j) => j!==i));setEditing(null);setForm(blank()); }}>{t("Delete")}</button></td></tr>;
   })}</tbody></table></div>
  <div className="card-body">
   <label className="form-label" htmlFor="model-test-account">{t("Account for request tests")}</label><select id="model-test-account" className="form-input" value={accountId} disabled={!!busy} onChange={e => setAccountId(e.target.value)}><option value="">{t("Select an account")}</option>{accounts.map(a => <option key={a.id} value={a.id}>{a.displayName || a.email || a.id} · {a.accountType === "personal" ? a.personalLicense : t("Work or school account")}</option>)}</select>
   <p style={{ fontSize: 12 }}>{t("Each test sends one short chat message and uses account quota. It does not publish the route or prove model identity. Test history is cleared on service restart.")}</p>
   <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(170px, 1fr))", gap: 10, marginTop: 16 }}>
    <label>{t("Public model")}<input className="form-input" value={form.publicModel} onChange={e => setForm({ ...form, publicModel: e.target.value })} /></label>
    <label>{t("Upstream tone")}<input className="form-input" list="discovered-model-tones" value={form.upstreamTone} onChange={e => setForm({ ...form, upstreamTone: e.target.value })} placeholder="Magic / Gpt_… / Claude_…" /><datalist id="discovered-model-tones">{tones.map(x => <option key={x} value={x} />)}</datalist></label>
    <label>{t("Display name")}<input className="form-input" value={form.displayName || ""} onChange={e => setForm({ ...form, displayName: e.target.value })} /></label>
    <label>{t("Route type")}<select className="form-input" value={form.routeKind || "model"} onChange={e => setForm({ ...form, routeKind: e.target.value })}><option value="model">{t("Model candidate")}</option><option value="mode">{t("Response mode")}</option></select></label>
    <label>{t("Account scope")}<select className="form-input" value={form.accountScope || "all"} onChange={e => setForm({ ...form, accountScope: e.target.value })}>{scopes.map(([id,label]) => <option key={id} value={id}>{t(label)}</option>)}</select></label>
    <label>{t("Upstream scenario (advanced)")}<select className="form-input" value={form.scenario || ""} onChange={e => setForm({ ...form, scenario: e.target.value })}><option value="">{t("Automatic by account")}</option>{["OfficeWebIncludedCopilot","OfficeWebPaidCopilot","OfficeWebFreeConsumerCopilot","OfficeWebPremiumConsumerCopilot"].map(x => <option key={x} value={x}>{x}</option>)}</select></label>
    <label>{t("Reasoning")}<select className="form-input" value={form.defaultReasoningLevel || "none"} onChange={e => setForm({ ...form, defaultReasoningLevel: e.target.value })}>{["none","minimal","low","medium","high","xhigh"].map(x => <option key={x} value={x}>{x}</option>)}</select></label>
   </div>
   <p style={{ fontSize: 12 }}>{t("An explicit scenario changes the upstream request context, not account permissions. Paid scenarios may be rejected; test them before publishing.")}</p>
   <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginTop: 12 }}><button className="btn primary" onClick={add}>{t(editing === null ? "Add route" : "Update route")}</button><button className="btn" disabled={!!busy || !accountId || !form.upstreamTone} onClick={() => void probe(form.upstreamTone, form.scenario)}>{t("Test request")}</button><button className="btn" onClick={() => { setEditing(null); setForm({ ...blank(), publicModel: "copilot-auto", displayName: t("Copilot automatic mode"), upstreamTone: "magic", routeKind: "mode" }); }}>{t("Add automatic mode")}</button></div>
   {message && <p role="status" aria-live="polite">{t(message)}</p>}
  </div>
 </div>;
}
