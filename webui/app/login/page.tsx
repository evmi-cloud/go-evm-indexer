"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { ConnectError } from "@connectrpc/connect";
import { client, tokenStore } from "@/lib/client";
import { resources } from "@/lib/resources";

export default function LoginPage() {
  const router = useRouter();
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [providers, setProviders] = useState<{ providerId: number; name: string; url: string }[]>([]);

  const home = `/${resources[0].key}`;

  // Complete an OAuth redirect (token / error arrive in the URL fragment).
  useEffect(() => {
    const hash = new URLSearchParams(window.location.hash.slice(1));
    if (hash.get("oauth_error")) {
      setError("OAuth sign-in failed.");
      history.replaceState(null, "", window.location.pathname);
      return;
    }
    const token = hash.get("token");
    if (token) {
      tokenStore.set(token);
      history.replaceState(null, "", window.location.pathname);
      client
        .me({})
        .then(() => router.replace(home))
        .catch(() => {
          tokenStore.clear();
          setError("OAuth sign-in failed.");
        });
    }
  }, [router, home]);

  // Offer a sign-in button per configured OAuth provider.
  useEffect(() => {
    client
      .listOAuthLoginUrls({})
      .then((r) => setProviders(r.options.map((o) => ({ providerId: o.providerId, name: o.name, url: o.url }))))
      .catch(() => setProviders([]));
  }, []);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const { token } = await client.login({ username, password });
      tokenStore.set(token);
      await client.me({});
      router.replace(home);
    } catch (err) {
      tokenStore.clear();
      setError(err instanceof ConnectError ? err.message : "login failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login-page">
      <div className="login-card">
        <div className="brand" style={{ fontSize: 26, textAlign: "center", marginBottom: 4 }}>
          EVMI
        </div>
        <p className="muted" style={{ textAlign: "center", marginTop: 0 }}>
          EVM Indexer control panel
        </p>

        <form onSubmit={submit}>
          <label>Username</label>
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" />
          <label>Password</label>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />
          <button type="submit" disabled={busy} style={{ width: "100%" }}>
            {busy ? "Signing in…" : "Sign in"}
          </button>
        </form>

        {providers.length > 0 && (
          <>
            <div className="or-divider">
              <span>or</span>
            </div>
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              {providers.map((p) => (
                <button
                  key={p.providerId}
                  className="secondary"
                  style={{ width: "100%" }}
                  onClick={() => (window.location.href = p.url)}
                >
                  Sign in with {p.name || "OAuth"}
                </button>
              ))}
            </div>
          </>
        )}

        {error && <div className="error" style={{ marginTop: 12 }}>{error}</div>}
      </div>
    </div>
  );
}
