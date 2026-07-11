import { client } from "@/lib/client";
import type { EvmLogSource } from "@/gen/evm_indexer/v1/evm_indexer_pb";
import { PAGE, big, bool, num, optNum, optStr, str, type FormValues, type Option, type Resource } from "./types";
import { abiEventArgOptions, abiEventOptions, abiOptions, abiTopic0Options, blockchainOptions, pipelineOptions } from "./options";

const sourceTypeOptions: Option[] = [
  { value: "CONTRACT", label: "Contract" },
  { value: "TOPIC", label: "Topic" },
  { value: "FACTORY", label: "Factory" },
  { value: "FULL", label: "Full chain" },
];

// Show a field only for the given source type(s).
const forType =
  (...types: string[]) =>
  (v: FormValues) =>
    types.includes(String(v.type));

export const sources: Resource<EvmLogSource> = {
  key: "sources",
  title: "Log sources",
  singular: "source",
  fields: [
    { name: "type", label: "Type", type: "select", options: sourceTypeOptions },
    { name: "enabled", label: "Enabled", type: "checkbox" },
    { name: "evmLogPipelineId", label: "Pipeline", type: "select", optionsFrom: pipelineOptions },
    { name: "evmBlockchainId", label: "Blockchain", type: "select", optionsFrom: blockchainOptions },
    { name: "startBlock", label: "Start block", type: "bigint" },
    // ABI is used for decoding (not needed for a full-chain source).
    { name: "evmJsonAbiId", label: "ABI", type: "select", optionsFrom: abiOptions, showIf: forType("CONTRACT", "TOPIC", "FACTORY") },
    // Contract / factory: the address to watch.
    { name: "address", label: "Contract address", type: "text", showIf: forType("CONTRACT", "FACTORY") },
    // Topic: pick the event from the selected ABI (stored as its topic0 hash).
    { name: "topic0", label: "Event (topic0)", type: "select", loadOptions: abiTopic0Options, depends: ["evmJsonAbiId"], help: "Derived from the selected ABI", showIf: forType("TOPIC") },
    { name: "topicFilters", label: "Indexed argument filters", type: "topicFilters", showIf: forType("TOPIC") },
    // Factory: how to discover child contracts.
    { name: "factoryChildEvmJsonAbi", label: "Child contract ABI", type: "select", optionsFrom: abiOptions, showIf: forType("FACTORY") },
    // Event / arg selects are derived from the child ABI selected above.
    { name: "factoryCreationFunctionName", label: "Creation event name", type: "select", loadOptions: abiEventOptions, depends: ["factoryChildEvmJsonAbi"], showIf: forType("FACTORY") },
    { name: "factoryCreationAddressLogArg", label: "Creation address arg", type: "select", loadOptions: abiEventArgOptions, depends: ["factoryChildEvmJsonAbi", "factoryCreationFunctionName"], showIf: forType("FACTORY") },
  ],
  columns: [
    { label: "ID", get: (s) => String(s.id ?? "") },
    { label: "Type", get: (s) => s.type },
    { label: "Target", get: (s) => s.address || s.topic0 || "—", mono: true },
    { label: "Sync block", get: (s) => String(s.syncBlock) },
    {
      label: "Status",
      get: (s) => (s.enabled ? s.status || "enabled" : "disabled"),
      tone: (s) => (!s.enabled ? "muted" : s.status === "RUNNING" ? "ok" : s.status === "LOOPBACKOFF" ? "warn" : "neutral"),
    },
  ],
  idOf: (s) => s.id ?? 0,
  list: async () => (await client.listEvmLogSources(PAGE)).sources ?? [],
  create: async (v) => {
    await client.createEvmLogSource({ source: sourceFromForm(v) });
  },
  update: async (id, v) => {
    await client.updateEvmLogSource({ source: { id, ...sourceFromForm(v) } });
  },
  remove: async (id) => {
    await client.deleteEvmLogSource({ id });
  },
  toForm: (s) => ({
    type: s.type,
    enabled: s.enabled,
    startBlock: String(s.startBlock),
    evmLogPipelineId: String(s.evmLogPipelineId),
    evmBlockchainId: String(s.evmBlockchainId),
    evmJsonAbiId: String(s.evmJsonAbiId),
    address: s.address ?? "",
    topic0: s.topic0 ?? "",
    topicFilters: (s.topicFilters ?? []).join("\n"),
    factoryChildEvmJsonAbi: s.factoryChildEvmJsonAbi != null ? String(s.factoryChildEvmJsonAbi) : "",
    factoryCreationFunctionName: s.factoryCreationFunctionName ?? "",
    factoryCreationAddressLogArg: s.factoryCreationAddressLogArg ?? "",
  }),
  actions: [
    { label: "Start", run: async (s) => void (await client.startSourceIndexer({ id: s.id ?? 0 })) },
    { label: "Stop", run: async (s) => void (await client.stopSourceIndexer({ id: s.id ?? 0 })) },
  ],
  // Live indexing progress via the server stream, with auto-reconnect.
  stream: (onUpdate, signal) => {
    void (async () => {
      while (!signal.aborted) {
        try {
          for await (const source of client.streamEvmLogSourceUpdates({ pipelineId: 0 }, { signal })) {
            onUpdate(source);
          }
        } catch {
          // disconnected (server restart / network); retry below unless aborted.
        }
        if (signal.aborted) return;
        await new Promise((resolve) => setTimeout(resolve, 2000));
      }
    })();
  },
};

function sourceFromForm(v: Parameters<Resource<EvmLogSource>["create"]>[0]) {
  return {
    type: str(v, "type"),
    enabled: bool(v, "enabled"),
    startBlock: big(v, "startBlock"),
    evmLogPipelineId: num(v, "evmLogPipelineId"),
    evmBlockchainId: num(v, "evmBlockchainId"),
    evmJsonAbiId: num(v, "evmJsonAbiId"),
    address: optStr(v, "address"),
    topic0: optStr(v, "topic0"),
    // Positional topics[1..] filters: keep interior blanks (wildcards) in place,
    // trim only trailing blanks. Do NOT use splitList — it drops empties.
    topicFilters: parseTopicFilters(str(v, "topicFilters")),
    factoryChildEvmJsonAbi: optNum(v, "factoryChildEvmJsonAbi"),
    factoryCreationFunctionName: optStr(v, "factoryCreationFunctionName"),
    factoryCreationAddressLogArg: optStr(v, "factoryCreationAddressLogArg"),
  };
}

// Split the newline-encoded topic filters, preserving interior wildcards (blank
// positions) and dropping only trailing blanks.
function parseTopicFilters(s: string): string[] {
  const parts = s.split("\n").map((x) => x.trim());
  while (parts.length && parts[parts.length - 1] === "") parts.pop();
  return parts;
}
