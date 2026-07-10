import { client } from "@/lib/client";
import type { EvmiExporter } from "@/gen/evm_indexer/v1/evm_indexer_pb";
import { PAGE, big, bool, num, str, type Resource } from "./types";
import { pipelineOptions, pluginOptions } from "./options";

export const exporters: Resource<EvmiExporter> = {
  key: "exporters",
  title: "Exporters",
  singular: "exporter",
  fields: [
    { name: "name", label: "Name", type: "text", required: true },
    { name: "evmLogPipelineId", label: "Pipeline", type: "select", optionsFrom: pipelineOptions },
    { name: "pluginId", label: "Plugin", type: "select", optionsFrom: pluginOptions, help: "Install plugins on the Plugins tab first" },
    { name: "enabled", label: "Enabled", type: "checkbox" },
    { name: "startBlock", label: "Start block", type: "bigint" },
    { name: "pluginConfigJson", label: "Plugin config", type: "pluginConfig", dependsOn: "pluginId" },
  ],
  columns: [
    { label: "ID", get: (e) => String(e.id ?? "") },
    { label: "Name", get: (e) => e.name },
    { label: "Pipeline", get: (e) => String(e.evmLogPipelineId) },
    { label: "Sync block", get: (e) => String(e.syncBlock) },
    {
      label: "Status",
      get: (e) => (e.enabled ? e.status || "enabled" : "disabled"),
      tone: (e) => (!e.enabled ? "muted" : e.status === "RUNNING" ? "ok" : e.status === "FAILED" ? "danger" : "neutral"),
    },
  ],
  idOf: (e) => e.id ?? 0,
  list: async () => (await client.listEvmiExporters(PAGE)).exporters ?? [],
  create: async (v) => {
    await client.createEvmiExporter({ exporter: exporterFromForm(v) });
  },
  update: async (id, v) => {
    await client.updateEvmiExporter({ exporter: { id, ...exporterFromForm(v) } });
  },
  remove: async (id) => {
    await client.deleteEvmiExporter({ id });
  },
  toForm: (e) => ({
    name: e.name,
    evmLogPipelineId: String(e.evmLogPipelineId),
    pluginId: String(e.pluginId),
    enabled: e.enabled,
    startBlock: String(e.startBlock),
    pluginConfigJson: e.pluginConfigJson,
  }),
  actions: [
    { label: "Start", run: async (e) => void (await client.startExporter({ id: e.id ?? 0 })) },
    { label: "Stop", run: async (e) => void (await client.stopExporter({ id: e.id ?? 0 })) },
  ],
  // Live sync progress / status via the server stream, with auto-reconnect.
  stream: (onUpdate, signal) => {
    void (async () => {
      while (!signal.aborted) {
        try {
          for await (const exporter of client.streamEvmiExporterUpdates({ pipelineId: 0 }, { signal })) {
            onUpdate(exporter);
          }
        } catch {
          // disconnected; retry below unless aborted.
        }
        if (signal.aborted) return;
        await new Promise((resolve) => setTimeout(resolve, 2000));
      }
    })();
  },
};

function exporterFromForm(v: Parameters<Resource<EvmiExporter>["create"]>[0]) {
  return {
    name: str(v, "name"),
    evmLogPipelineId: num(v, "evmLogPipelineId"),
    pluginId: num(v, "pluginId"),
    enabled: bool(v, "enabled"),
    startBlock: big(v, "startBlock"),
    pluginConfigJson: str(v, "pluginConfigJson"),
  };
}
