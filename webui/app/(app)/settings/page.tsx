"use client";

import { useEffect, useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { client } from "@/lib/client";
import { useAuth, isAdmin } from "@/lib/auth-context";

type Form = {
  enabled: boolean;
  provider: string;
  clientId: string;
  clientSecret: string;
  authUrl: string;
  tokenUrl: string;
  userInfoUrl: string;
  redirectUrl: string;
  scopes: string;
};

const empty: Form = {
  enabled: false,
  provider: "",
  clientId: "",
  clientSecret: "",
  authUrl: "",
  tokenUrl: "",
  userInfoUrl: "",
  redirectUrl: "",
  scopes: "openid email profile",
};

// A quick-fill for Google to make setup obvious.
const googlePreset: Partial<Form> = {
  provider: "google",
  authUrl: "https://accounts.google.com/o/oauth2/v2/auth",
  tokenUrl: "https://oauth2.googleapis.com/token",
  userInfoUrl: "https://openidconnect.googleapis.com/v1/userinfo",
  scopes: "openid email profile",
};

export default function SettingsPage() {
  const user = useAuth();
  const [form, setForm] = useState<Form>(empty);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!isAdmin(user)) {
      setLoading(false);
      return;
    }
    client
      .getOAuthConfig({})
      .then((r) => {
        const c = r.config;
        if (c)
          setForm((f) => ({
            ...f,
            enabled: c.enabled,
            provider: c.provider,
            clientId: c.clientId,
            authUrl: c.authUrl,
            tokenUrl: c.tokenUrl,
            userInfoUrl: c.userInfoUrl,
            redirectUrl: c.redirectUrl || defaultRedirect(),
            scopes: c.scopes || f.scopes,
          }));
      })
      .catch((err) => setError(err instanceof ConnectError ? err.message : "failed to load config"))
      .finally(() => setLoading(false));
  }, [user]);

  function set<K extends keyof Form>(key: K, value: Form[K]) {
    setForm((f) => ({ ...f, [key]: value }));
    setSaved(false);
  }

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError(null);
    setSaved(false);
    try {
      await client.updateOAuthConfig(form);
      setForm((f) => ({ ...f, clientSecret: "" })); // never keep the secret in memory
      setSaved(true);
    } catch (err) {
      setError(err instanceof ConnectError ? err.message : "failed to save");
    } finally {
      setSaving(false);
    }
  }

  if (!isAdmin(user))
    return (
      <div>
        <div className="page-header">
          <h2>Settings</h2>
        </div>
        <div className="empty muted">Admin role required.</div>
      </div>
    );

  return (
    <div>
      <div className="page-header">
        <div>
          <h2>OAuth login</h2>
          <p className="subtitle">Let users sign in through an external OAuth2 / OIDC provider.</p>
        </div>
      </div>

      {loading ? (
        <div className="empty muted">Loading…</div>
      ) : (
        <form className="panel form-card" onSubmit={save}>
          <label className="checkbox">
            <input type="checkbox" checked={form.enabled} onChange={(e) => set("enabled", e.target.checked)} />
            Enable OAuth login
          </label>

          <div className="row" style={{ justifyContent: "flex-start", gap: 8, marginBottom: 14 }}>
            <span className="muted" style={{ fontSize: 13 }}>
              Presets:
            </span>
            <button type="button" className="secondary small" onClick={() => setForm((f) => ({ ...f, ...googlePreset }))}>
              Google
            </button>
          </div>

          <Field label="Provider name" value={form.provider} onChange={(v) => set("provider", v)} placeholder="google" />
          <Field label="Client ID" value={form.clientId} onChange={(v) => set("clientId", v)} />
          <Field
            label="Client secret"
            type="password"
            value={form.clientSecret}
            onChange={(v) => set("clientSecret", v)}
            help="Leave blank to keep the stored secret."
          />
          <Field label="Authorization URL" value={form.authUrl} onChange={(v) => set("authUrl", v)} mono />
          <Field label="Token URL" value={form.tokenUrl} onChange={(v) => set("tokenUrl", v)} mono />
          <Field label="User info URL" value={form.userInfoUrl} onChange={(v) => set("userInfoUrl", v)} mono />
          <Field
            label="Redirect URL"
            value={form.redirectUrl}
            onChange={(v) => set("redirectUrl", v)}
            mono
            help="Register this exact URL with your provider."
          />
          <Field label="Scopes" value={form.scopes} onChange={(v) => set("scopes", v)} help="Space-separated." />

          {error && <div className="error">{error}</div>}
          <div className="row" style={{ marginTop: 12 }}>
            {saved && <span className="saved-note">Saved ✓</span>}
            <button type="submit" disabled={saving}>
              {saving ? "Saving…" : "Save configuration"}
            </button>
          </div>
        </form>
      )}
    </div>
  );
}

function defaultRedirect() {
  return typeof window !== "undefined" ? `${window.location.origin}/auth/oauth/callback` : "";
}

function Field({
  label,
  value,
  onChange,
  type = "text",
  placeholder,
  help,
  mono,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  type?: string;
  placeholder?: string;
  help?: string;
  mono?: boolean;
}) {
  return (
    <div>
      <label>{label}</label>
      <input
        type={type}
        value={value}
        placeholder={placeholder}
        className={mono ? "mono" : undefined}
        onChange={(e) => onChange(e.target.value)}
      />
      {help && <p className="field-help">{help}</p>}
    </div>
  );
}
