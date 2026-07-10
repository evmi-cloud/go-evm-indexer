"use client";

import { createContext, useContext } from "react";
import type { AuthUser } from "@/gen/evm_indexer/v1/evm_indexer_pb";

export const AuthContext = createContext<AuthUser | null>(null);

/** The authenticated user, provided by the (app) layout. */
export function useAuth(): AuthUser | null {
  return useContext(AuthContext);
}

export function isAdmin(user: AuthUser | null): boolean {
  return user?.role === "admin";
}
