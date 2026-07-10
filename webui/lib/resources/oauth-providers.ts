import { client } from "@/lib/client";
import type { OAuthProvider } from "@/gen/evm_indexer/v1/evm_indexer_pb";
import { bool, str, type FormValues, type Resource } from "./types";

export const oauthProviders: Resource<OAuthProvider> = {
  key: "oauth-providers",
  title: "OAuth providers",
  singular: "provider",
  adminOnly: true,
  fields: [
    { name: "name", label: "Name", type: "text", required: true, help: "Shown on the sign-in button" },
    { name: "enabled", label: "Enabled", type: "checkbox" },
    { name: "clientId", label: "Client ID", type: "text" },
    { name: "clientSecret", label: "Client secret", type: "password", help: "On edit, leave blank to keep the stored secret." },
    { name: "authUrl", label: "Authorization URL", type: "text" },
    { name: "tokenUrl", label: "Token URL", type: "text" },
    { name: "userInfoUrl", label: "User info URL", type: "text" },
    { name: "redirectUrl", label: "Redirect URL", type: "text", help: "e.g. https://your-host/auth/oauth/callback" },
    { name: "scopes", label: "Scopes", type: "text", help: "Space-separated, e.g. openid email profile" },
  ],
  columns: [
    { label: "ID", get: (p) => String(p.id ?? "") },
    { label: "Name", get: (p) => p.name },
    { label: "Client ID", get: (p) => p.clientId || "—", mono: true },
    { label: "Status", get: (p) => (p.enabled ? "enabled" : "disabled"), tone: (p) => (p.enabled ? "ok" : "muted") },
  ],
  idOf: (p) => p.id ?? 0,
  list: async () => (await client.listOAuthProviders({})).providers ?? [],
  create: async (v) => {
    await client.createOAuthProvider({ provider: providerFromForm(v), clientSecret: str(v, "clientSecret") });
  },
  update: async (id, v) => {
    await client.updateOAuthProvider({ provider: { id, ...providerFromForm(v) }, clientSecret: str(v, "clientSecret") });
  },
  remove: async (id) => {
    await client.deleteOAuthProvider({ id });
  },
  toForm: (p) => ({
    name: p.name,
    enabled: p.enabled,
    clientId: p.clientId,
    clientSecret: "",
    authUrl: p.authUrl,
    tokenUrl: p.tokenUrl,
    userInfoUrl: p.userInfoUrl,
    redirectUrl: p.redirectUrl,
    scopes: p.scopes,
  }),
};

function providerFromForm(v: FormValues) {
  return {
    enabled: bool(v, "enabled"),
    name: str(v, "name"),
    clientId: str(v, "clientId"),
    authUrl: str(v, "authUrl"),
    tokenUrl: str(v, "tokenUrl"),
    userInfoUrl: str(v, "userInfoUrl"),
    redirectUrl: str(v, "redirectUrl"),
    scopes: str(v, "scopes"),
  };
}
