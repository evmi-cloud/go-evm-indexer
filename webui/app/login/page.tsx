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
  const [oauthUrl, setOauthUrl] = useState<string | null>(null);
  const [oauthProvider, setOauthProvider] = useState<string>("");

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

  // Offer OAuth only when a provider is configured.
  useEffect(() => {
    client
      .getOAuthLoginUrl({})
      .then((r) => setOauthUrl(r.url))
      .catch(() => setOauthUrl(null));
    client
      .getOAuthConfig({})
      .then((r) => setOauthProvider(r.config?.provider ?? ""))
      .catch(() => {});
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

        {oauthUrl && (
          <>
            <div className="or-divider">
              <span>or</span>
            </div>
            <button className="secondary" style={{ width: "100%" }} onClick={() => (window.location.href = oauthUrl)}>
              Sign in with {oauthProvider || "OAuth"}
            </button>
          </>
        )}

        {error && <div className="error" style={{ marginTop: 12 }}>{error}</div>}
      </div>
    </div>
  );
}
