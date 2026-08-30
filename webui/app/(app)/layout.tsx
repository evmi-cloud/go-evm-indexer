"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { client, tokenStore } from "@/lib/client";
import { resources } from "@/lib/resources";
import { AuthContext, isAdmin } from "@/lib/auth-context";
import type { AuthUser } from "@/gen/evm_indexer/v1/evm_indexer_pb";

// Authenticated shell: guards every resource route, provides the user via
// context, and renders the sidebar nav.
export default function AppLayout({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const [user, setUser] = useState<AuthUser | null>(null);
  const [checked, setChecked] = useState(false);

  useEffect(() => {
    if (!tokenStore.get()) {
      router.replace("/login");
      return;
    }
    client
      .me({})
      .then((r) => setUser(r.user ?? null))
      .catch(() => {
        tokenStore.clear();
        router.replace("/login");
      })
      .finally(() => setChecked(true));
  }, [router]);

  if (!checked || !user) {
    return (
      <div className="loading-screen">
        <div className="brand">EVMI</div>
      </div>
    );
  }

  function logout() {
    tokenStore.clear();
    router.replace("/login");
  }

  // Download the current configuration (blockchains, ABIs, stores, pipelines,
  // sources — excluding factory-created ones — plugins and exporters) as an
  // autoloader-compatible JSON file.
  async function exportConfig() {
    try {
      const res = await client.exportConfiguration({});
      const blob = new Blob([res.configJson], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "evmi-config.json";
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (err) {
      alert(`Export failed: ${err instanceof Error ? err.message : String(err)}`);
    }
  }

  const isActive = (href: string) => pathname === href || pathname === `${href}/`;

  return (
    <AuthContext.Provider value={user}>
      <div className="shell">
        <aside className="sidebar">
          <div className="brand">EVMI</div>
          <nav>
            <div className="nav-label">Configuration</div>
            {resources
              .filter((r) => !r.adminOnly)
              .map((r) => (
                <Link key={r.key} href={`/${r.key}`} className={isActive(`/${r.key}`) ? "active" : ""}>
                  {r.title}
                </Link>
              ))}
            <div className="nav-label">Data</div>
            <Link href="/logs" className={isActive("/logs") ? "active" : ""}>
              Logs
            </Link>
            {isAdmin(user) && (
              <>
                <div className="nav-label">Admin</div>
                {resources
                  .filter((r) => r.adminOnly)
                  .map((r) => (
                    <Link key={r.key} href={`/${r.key}`} className={isActive(`/${r.key}`) ? "active" : ""}>
                      {r.title}
                    </Link>
                  ))}
              </>
            )}
          </nav>
          <div className="sidebar-footer">
            <div className="user">
              <span className="avatar">{user.username.slice(0, 1).toUpperCase()}</span>
              <div>
                <div className="user-name">{user.username}</div>
                <div className="user-role">{user.role}</div>
              </div>
            </div>
            {isAdmin(user) && (
              <button className="secondary small" onClick={exportConfig}>
                Export config
              </button>
            )}
            <button className="secondary small" onClick={logout}>
              Sign out
            </button>
          </div>
        </aside>
        <section className="content">{children}</section>
      </div>
    </AuthContext.Provider>
  );
}
