import { client } from "@/lib/client";
import type { Plugin } from "@/gen/evm_indexer/v1/evm_indexer_pb";
import { PAGE, str, type Resource } from "./types";
import { gitRefOptions } from "./options";

// Plugins install from a git repository only: the server clones it at the chosen
// branch/tag and builds the RelativePath package.
export const plugins: Resource<Plugin> = {
  key: "plugins",
  title: "Plugins",
  singular: "plugin",
  fields: [
    { name: "name", label: "Name", type: "text", required: true },
    { name: "description", label: "Description", type: "text" },
    { name: "gitUrl", label: "Git URL", type: "text", required: true, help: "Any git repo — cloned and built" },
    {
      name: "gitRef",
      label: "Branch or tag",
      type: "select",
      loadOptions: gitRefOptions,
      depends: ["gitUrl"],
      help: "Fetched from the repository — empty uses the default branch",
    },
  ],
  columns: [
    { label: "ID", get: (p) => String(p.id ?? "") },
    { label: "Name", get: (p) => p.name },
    { label: "Source", get: (p) => (p.gitUrl ? (p.gitRef ? `${p.gitUrl} @ ${p.gitRef}` : p.gitUrl) : "—"), mono: true },
    {
      label: "Status",
      get: (p) => p.status || "NOT_INSTALLED",
      tone: (p) =>
        p.status === "INSTALLED"
          ? "ok"
          : p.status === "FAILED"
            ? "danger"
            : p.status === "INSTALLING"
              ? "warn"
              : "muted",
    },
  ],
  idOf: (p) => p.id ?? 0,
  list: async () => (await client.listPlugins(PAGE)).plugins ?? [],
  create: async (v) => {
    await client.createPlugin({ plugin: pluginFromForm(v) });
  },
  update: async (id, v) => {
    await client.updatePlugin({ plugin: { id, ...pluginFromForm(v) } });
  },
  remove: async (id) => {
    await client.deletePlugin({ id });
  },
  toForm: (p) => ({
    name: p.name,
    description: p.description,
    gitUrl: p.gitUrl,
    gitRef: p.gitRef,
  }),
  actions: [
    // Build the shared object; the row's status reflects the outcome on refresh.
    { label: "Install", run: async (p) => void (await client.installPlugin({ id: p.id ?? 0 })) },
  ],
};

function pluginFromForm(v: Parameters<Resource<Plugin>["create"]>[0]) {
  return {
    name: str(v, "name"),
    description: str(v, "description"),
    gitUrl: str(v, "gitUrl"),
    gitRef: str(v, "gitRef"),
  };
}
