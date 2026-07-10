import { client } from "@/lib/client";
import type { AccessTokenInfo } from "@/gen/evm_indexer/v1/evm_indexer_pb";
import { num, str, type Resource } from "./types";

function unixToDate(v?: bigint): string {
  return v ? new Date(Number(v) * 1000).toLocaleString() : "—";
}

// Access tokens are opaque API keys tied to the signed-in user; using one grants
// that user's access (role and all). Every user manages their own here.
export const accessTokens: Resource<AccessTokenInfo> = {
  key: "access-tokens",
  title: "Access tokens",
  singular: "access token",
  fields: [
    { name: "name", label: "Name", type: "text", required: true, help: "A label to recognize this key" },
    { name: "expiresInDays", label: "Expires in (days)", type: "number", help: "Leave 0 for no expiry" },
  ],
  columns: [
    { label: "ID", get: (t) => String(t.id) },
    { label: "Name", get: (t) => t.name },
    { label: "Created", get: (t) => unixToDate(t.createdAt) },
    { label: "Expires", get: (t) => unixToDate(t.expiresAt) },
    { label: "Last used", get: (t) => unixToDate(t.lastUsedAt) },
  ],
  idOf: (t) => t.id,
  list: async () => (await client.listAccessTokens({})).tokens ?? [],
  // Returns the plaintext token, which ResourceManager shows once.
  create: async (v) => {
    const res = await client.createAccessToken({ name: str(v, "name"), expiresInDays: num(v, "expiresInDays") });
    return res.token;
  },
  // No update — tokens are immutable (create a new one instead).
  remove: async (id) => {
    await client.revokeAccessToken({ id });
  },
};
