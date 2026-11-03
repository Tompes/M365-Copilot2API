import { useEffect, useState } from "react";
import { api } from "../api";
import { ModelSettings, type ModelMapping } from "./ModelSettings";
import { t, LOCALES, setLocale, getLocale, type Locale } from "../i18n";

const FLAGS: [string, string][] = [
  ["enableMemoryV2", "Memory V2"],
  ["enableDeepWork", "Deep Work"],
  ["enableComputerUse", "Computer Use"],
  ["enableRealtimeVoice", "Realtime Voice"],
  ["enableSystemPromptOverride", "System Prompt Override"],
  ["enableImageApi", "Image API"],
  ["enableDesignerImageGen4o", "Designer Image 4o"],
  ["enableCodeCanvas", "Code Canvas"],
  ["enableSydneyReconnect", "Sydney Reconnect"],
];

const NUM_FIELDS: { key: string; id: string; label: string }[] = [
  { key: "chatTimeoutSeconds", id: "fChatTimeout", label: "Chat timeout (5-3600s)" },
  { key: "imageTimeoutSeconds", id: "fImageTimeout", label: "Image timeout (5-3600s)" },
  { key: "contextWindow", id: "fContextWindow", label: "Context window" },
  { key: "maxOutputTokens", id: "fMaxOutput", label: "Max output tokens" },
  { key: "maxToolCallsPerTurn", id: "fMaxToolCalls", label: "Max tool calls/turn (1-64)" },
  { key: "maxToolRounds", id: "fMaxToolRounds", label: "Max tool rounds (1-512)" },
  { key: "maxConversationMessages", id: "fMaxConv", label: "Max conversation messages" },
  { key: "accountConcurrencyLimit", id: "fAcctConc", label: "Account concurrency (1-64)" },
  { key: "rateLimitCooldownSeconds", id: "fCooldown", label: "Rate limit cooldown (5-3600s)" },
  { key: "transientThrottledCooldownSeconds", id: "fTransient", label: "Transient throttle cooldown (5-600s)" },
];

type Mapping = ModelMapping;

export function SettingsPage({ push }: { push: (m: string, k?: "success" | "error" | "info") => void }) {
  const [v, setV] = useState<Record<string, any>>({});
  const [mappings, setMappings] = useState<Mapping[]>([]);
  const [hideBuiltins, setHideBuiltins] = useState(false);
  const [autoStart, setAutoStart] = useState(false);
  const [autoStartSupported, setAutoStartSupported] = useState(true);
  const [locale, setLocaleState] = useState<Locale>(getLocale());

  const load = async () => {
    try {
      const d = await api("/api/admin/settings");
      const s = d.settings ?? {};
      setV(s);
      setMappings(s.modelMappings ?? []);
      setHideBuiltins(!!s.hideBuiltinModels);
      setAutoStartSupported(d.autoStartSupported !== false);
      setAutoStart(!!d.autoStart);
    } catch (e: any) {
      push(String(e?.message ?? e), "error");
    }
  };

  useEffect(() => {
    load();
  }, []);

  const num = (id: string): number | undefined => {
    const el = document.getElementById(id) as HTMLInputElement | null;
    const raw = el?.value.trim();
    if (!raw) return undefined;
    const n = parseInt(raw, 10);
    return Number.isNaN(n) ? undefined : n;
  };

  const save = async () => {
    try {
      const body: Record<string, unknown> = { modelMappings: mappings, hideBuiltinModels: hideBuiltins };
      for (const f of NUM_FIELDS) {
        const n = num(f.id);
        if (n !== undefined) body[f.key] = n;
      }
      body.scenario = (document.getElementById("fScenario") as HTMLSelectElement)?.value;
      body.licenseType = (document.getElementById("fLicense") as HTMLSelectElement)?.value;
      body.logLevel = (document.getElementById("fLogLevel") as HTMLSelectElement)?.value;
      body.listenAddress = (document.getElementById("fListen") as HTMLInputElement)?.value.trim() || undefined;
      document.querySelectorAll<HTMLInputElement>("input[data-ff]").forEach((c) => {
        body[c.dataset.ff!] = c.checked;
      });
      if (autoStartSupported) body.autoStart = autoStart;
      const resp = await api("/api/admin/settings", { method: "PUT", body: JSON.stringify(body) });
      push(t("Settings saved"), "success");
      if (resp?.autoStartError) push(String(resp.autoStartError), "error");
      else if (resp?.autoStartApplied === true) push(t("Auto start enabled"), "success");
      else if (resp?.autoStartApplied === false && resp?.autoStart === false) push(t("Auto start disabled"), "info");
    } catch (e: any) {
      push(String(e?.message ?? e), "error");
    }
  };

  const gr = (k: string) => v[k] ?? "";
  const gc = (k: string) => !!v[k];

  return (
    <div>
      {/* Requests & models */}
      <div className="card">
        <div className="card-head"><span>{t("Requests & models")}</span></div>
        <div className="card-body" style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(210px, 1fr))", gap: 14 }}>
          {NUM_FIELDS.slice(0, 7).map((f) => (
            <div className="form-group" style={{ margin: 0 }} key={f.id}>
              <label className="form-label" htmlFor={f.id}>{t(f.label)}</label>
              <input className="form-input" id={f.id} type="number" defaultValue={gr(f.key) as number} key={`${f.id}-${gr(f.key)}`} />
            </div>
          ))}
        </div>
      </div>

      {/* Accounts & limits */}
      <div className="card">
        <div className="card-head"><span>{t("Accounts & limits")}</span></div>
        <div className="card-body" style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(210px, 1fr))", gap: 14 }}>
          {NUM_FIELDS.slice(7).map((f) => (
            <div className="form-group" style={{ margin: 0 }} key={f.id}>
              <label className="form-label" htmlFor={f.id}>{t(f.label)}</label>
              <input className="form-input" id={f.id} type="number" defaultValue={gr(f.key) as number} key={`${f.id}-${gr(f.key)}`} />
            </div>
          ))}
          <div className="form-group" style={{ margin: 0 }}>
            <label className="form-label" htmlFor="fScenario">{t("Scenario")}</label>
            <select className="form-input" id="fScenario" defaultValue={gr("scenario") as string}>
              {["OfficeWebIncludedCopilot", "Bizchat", "CopilotConsumer", "Chathub"].map((s) => (
                <option key={s} value={s}>{s}</option>
              ))}
            </select>
          </div>
          <div className="form-group" style={{ margin: 0 }}>
            <label className="form-label" htmlFor="fLicense">{t("License")}</label>
            <select className="form-input" id="fLicense" defaultValue={gr("licenseType") as string}>
              {["Starter", "Premium", "Free", "BCAIS", "BCSWW", "BCWAF", "BCWBF"].map((s) => (
                <option key={s} value={s}>{s}</option>
              ))}
            </select>
          </div>
          <div className="form-group" style={{ margin: 0 }}>
            <label className="form-label" htmlFor="fLogLevel">{t("Log level")}</label>
            <select className="form-input" id="fLogLevel" defaultValue={gr("logLevel") as string}>
              {["silent", "error", "warn", "info", "debug"].map((s) => (
                <option key={s} value={s}>{s}</option>
              ))}
            </select>
          </div>
        </div>
      </div>

      {/* Feature flags */}
      <div className="card">
        <div className="card-head"><span>{t("Feature flags")}</span></div>
        <div className="card-body" style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(210px, 1fr))", gap: 8 }}>
          {FLAGS.map(([key, label]) => (
            <label key={key} style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12.5 }}>
              <input type="checkbox" data-ff={key} defaultChecked={gc(key)} key={`ff-${key}-${gc(key)}`} />
              {t(label)}
            </label>
          ))}
        </div>
      </div>

      <ModelSettings mappings={mappings} setMappings={setMappings} hideBuiltins={hideBuiltins} setHideBuiltins={setHideBuiltins} push={push} />

      {/* System */}
      <div className="card">
        <div className="card-head"><span>{t("System")}</span></div>
        <div className="card-body" style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(210px, 1fr))", gap: 14, alignItems: "end" }}>
          <div className="form-group" style={{ margin: 0 }}>
            <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12.5 }}>
              <input type="checkbox" checked={autoStart} onChange={(e) => setAutoStart(e.target.checked)} disabled={!autoStartSupported} />
              {t("Auto start")}
            </label>
          </div>
          <div className="form-group" style={{ margin: 0 }}>
            <label className="form-label">{t("Log location")}</label>
            <input className="form-input" readOnly value={gr("debugLogPath") ? String(gr("debugLogPath")) : "stdout / stderr (M365_DEBUG_LOG)"} style={{ fontSize: 12 }} />
          </div>
        </div>
      </div>

      {/* General */}
      <div className="card">
        <div className="card-body" style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(210px, 1fr))", gap: 14 }}>
          <div className="form-group" style={{ margin: 0 }}>
            <label className="form-label" htmlFor="fListen">{t("Listen address (restart required)")}</label>
            <input className="form-input" id="fListen" defaultValue={gr("listenAddress") as string} placeholder="127.0.0.1:4141" key={`l-${gr("listenAddress")}`} />
          </div>
          <div className="form-group" style={{ margin: 0 }}>
            <label className="form-label" htmlFor="fLocale">{t("Language")}</label>
            <select
              className="form-input"
              id="fLocale"
              value={locale}
              onChange={(e) => {
                const l = e.target.value as Locale;
                setLocale(l);
                setLocaleState(l);
              }}
            >
              {LOCALES.map((l) => (<option key={l.id} value={l.id}>{l.label}</option>))}
            </select>
          </div>
        </div>
        <div className="card-body" style={{ display: "flex", gap: 8, borderTop: "1px solid var(--line)" }}>
          <button className="btn primary" onClick={save}>{t("Save settings")}</button>
          <button className="btn" onClick={load}>{t("Reload")}</button>
        </div>
      </div>
    </div>
  );
}
