import { client } from "@/lib/client";
import type { EvmLogSource } from "@/gen/evm_indexer/v1/evm_indexer_pb";
import { PAGE, big, bool, num, optStr, str, type FormValues, type Option, type Resource } from "./types";
import { abiOptions, abiTopic0Options, blockchainOptions, pipelineOptions } from "./options";

// A FactoryRule init shape (keys match the proto message; recursive).
type FactoryRuleInit = {
  creationFunctionName?: string;
  creationAddressLogArg?: string;
  childType?: string;
  evmJsonAbiId?: number;
  conditions?: { arg?: string; operator?: string; value?: string }[];
  childRules?: FactoryRuleInit[];
};

// parseFactoryRules turns the rules-editor JSON back into the FactoryRule array
// the API expects (keys already match the proto message fields).
function parseFactoryRules(s: string): FactoryRuleInit[] {
  try {
    const parsed = JSON.parse(s || "[]");
    return Array.isArray(parsed) ? (parsed as FactoryRuleInit[]) : [];
  } catch {
    return [];
  }
}

// shortHex truncates a long 0x value (address/topic) to `0x1234…cdef`; short
// values are returned unchanged.
function shortHex(v: string): string {
  return v.length > 12 ? `${v.slice(0, 6)}…${v.slice(-4)}` : v;
}

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
    // Factory: N creation rules (each: creation event → child of a type/ABI).
    // A rule that creates a factory carries its own nested rules (recursive).
    { name: "factoryRules", label: "Factory rules", type: "factoryRules", showIf: forType("FACTORY") },
  ],
  columns: [
    { label: "ID", get: (s) => String(s.id ?? "") },
    { label: "Type", get: (s) => s.type },
    // Address (or topic0) shown truncated; hover shows the full value (title attr).
    {
      label: "Target",
      get: (s) => (s.address ? shortHex(s.address) : s.topic0 ? shortHex(s.topic0) : "—"),
      title: (s) => s.address || s.topic0 || "",
      mono: true,
    },
    // Contract ABI name, resolved from the loaded abi id→name map.
    {
      label: "Contract",
      get: (s, ctx) => (ctx.abiNames as Record<number, string> | undefined)?.[s.evmJsonAbiId ?? 0] || "—",
    },
    { label: "Sync block", get: (s) => String(s.syncBlock) },
    {
      label: "Status",
      get: (s) => (s.enabled ? s.status || "enabled" : "disabled"),
      tone: (s) => (!s.enabled ? "muted" : s.status === "RUNNING" ? "ok" : s.status === "LOOPBACKOFF" ? "warn" : "neutral"),
    },
  ],
  // Load abi id→name so the Contract column can label each source's ABI.
  loadColumnContext: async () => {
    const abis = (await client.listEvmJsonAbis(PAGE)).abis ?? [];
    const abiNames: Record<number, string> = {};
    for (const a of abis) abiNames[a.id ?? 0] = a.contractName;
    return { abiNames };
  },
  idOf: (s) => s.id ?? 0,
  // Factory-created sources carry the id of the factory source that spawned them,
  // so the list can be rendered as a hierarchy (children nested under the factory).
  parentIdOf: (s) => s.parentSourceId ?? 0,
  // A factory-created child source is managed by its parent factory: it can only be
  // started/stopped, not edited or deleted.
  readOnly: (s) => (s.parentSourceId ?? 0) !== 0,
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
    factoryRules: JSON.stringify(s.factoryRules ?? []),
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
    factoryRules: parseFactoryRules(str(v, "factoryRules")),
  };
}

// Split the newline-encoded topic filters, preserving interior wildcards (blank
// positions) and dropping only trailing blanks.
function parseTopicFilters(s: string): string[] {
  const parts = s.split("\n").map((x) => x.trim());
  while (parts.length && parts[parts.length - 1] === "") parts.pop();
  return parts;
}
