"use client";

import { createClient, type Interceptor } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { EvmIndexerService } from "@/gen/evm_indexer/v1/evm_indexer_pb";

const TOKEN_KEY = "evmi_token";

export const tokenStore = {
  get: () => (typeof window === "undefined" ? null : window.localStorage.getItem(TOKEN_KEY)),
  set: (t: string) => window.localStorage.setItem(TOKEN_KEY, t),
  clear: () => window.localStorage.removeItem(TOKEN_KEY),
};

// Attach the stored bearer token to every request. Login/GetOAuthLoginUrl are
// public, so a missing token simply sends no header.
const authInterceptor: Interceptor = (next) => async (req) => {
  const token = tokenStore.get();
  if (token) req.header.set("Authorization", `Bearer ${token}`);
  return next(req);
};

// Same-origin by default (the app is served by the Go server); override with
// NEXT_PUBLIC_API_BASE for `next dev` against a remote server.
function baseUrl(): string {
  if (process.env.NEXT_PUBLIC_API_BASE) return process.env.NEXT_PUBLIC_API_BASE;
  if (typeof window !== "undefined") return window.location.origin;
  return "http://localhost:8080";
}

const transport = createConnectTransport({
  baseUrl: baseUrl(),
  interceptors: [authInterceptor],
});

/** Fully-typed client for the EVMI Connect API. */
export const client = createClient(EvmIndexerService, transport);
