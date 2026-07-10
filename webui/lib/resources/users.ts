import { client } from "@/lib/client";
import type { AuthUser } from "@/gen/evm_indexer/v1/evm_indexer_pb";
import { str, type Resource } from "./types";

export const users: Resource<AuthUser> = {
  key: "users",
  title: "Users",
  singular: "user",
  adminOnly: true,
  fields: [
    { name: "username", label: "Username", type: "text", required: true },
    { name: "email", label: "Email", type: "text" },
    {
      name: "role",
      label: "Role",
      type: "select",
      options: [
        { value: "user", label: "User" },
        { value: "admin", label: "Admin" },
      ],
    },
    { name: "password", label: "Password", type: "password", help: "On edit, leave blank to keep the current password." },
  ],
  columns: [
    { label: "ID", get: (u) => String(u.id) },
    { label: "Username", get: (u) => u.username },
    { label: "Email", get: (u) => u.email || "—" },
    { label: "Role", get: (u) => u.role, tone: (u) => (u.role === "admin" ? "neutral" : "muted") },
  ],
  idOf: (u) => u.id,
  list: async () => (await client.listUsers({})).users ?? [],
  create: async (v) => {
    await client.createUser({ username: str(v, "username"), email: str(v, "email"), role: str(v, "role"), password: str(v, "password") });
  },
  update: async (id, v) => {
    await client.updateUser({ id, username: str(v, "username"), email: str(v, "email"), role: str(v, "role"), password: str(v, "password") });
  },
  remove: async (id) => {
    await client.deleteUser({ id });
  },
  toForm: (u) => ({ username: u.username, email: u.email, role: u.role || "user", password: "" }),
};
