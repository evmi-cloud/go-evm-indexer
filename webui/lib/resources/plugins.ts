import { client } from "@/lib/client";
import type { Plugin } from "@/gen/evm_indexer/v1/evm_indexer_pb";
import { PAGE, str, type Resource } from "./types";

export const plugins: Resource<Plugin> = {
  key: "plugins",
  title: "Plugins",
  singular: "plugin",
  fields: [
    { name: "name", label: "Name", type: "text", required: true },
    { name: "description", label: "Description", type: "text" },
    {
      name: "localPath",
      label: "Local path (.so or module dir)",
      type: "text",
      help: "A prebuilt .so is used directly; a directory is built.",
    },
    { name: "githubUrl", label: "GitHub URL", type: "text", help: "Cloned and built if no .so is given" },
    { name: "relativePath", label: "Package path", type: "text", help: "Package within the module to build" },
  ],
  columns: [
    { label: "ID", get: (p) => String(p.id ?? "") },
    { label: "Name", get: (p) => p.name },
    { label: "Source", get: (p) => p.localPath || p.githubUrl || "—", mono: true },
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
    localPath: p.localPath,
    githubUrl: p.githubUrl,
    relativePath: p.relativePath,
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
    localPath: str(v, "localPath"),
    githubUrl: str(v, "githubUrl"),
    relativePath: str(v, "relativePath"),
  };
}
