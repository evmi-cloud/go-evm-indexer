// Async option loaders for relation (foreign-key) select fields.

import { keccak256 } from "js-sha3";
import { client } from "@/lib/client";
import { PAGE, type FormValues, type Option } from "./types";

export const abiOptions = async (): Promise<Option[]> =>
  ((await client.listEvmJsonAbis(PAGE)).abis ?? []).map((a) => ({
    value: String(a.id ?? 0),
    label: `${a.contractName} (#${a.id})`,
  }));

// gitRefOptions lists a plugin repo's branches and tags (the server runs
// `git ls-remote`) for the git-ref select. Keeps the currently-selected ref in
// the list even if the repo no longer advertises it, so an edit doesn't lose it.
export const gitRefOptions = async (values: FormValues): Promise<Option[]> => {
  const gitUrl = String(values.gitUrl ?? "").trim();
  if (!gitUrl) return [];
  const r = await client.listPluginGitRefs({ gitUrl });
  const opts: Option[] = [
    ...(r.branches ?? []).map((b) => ({ value: b, label: `${b} (branch)` })),
    ...(r.tags ?? []).map((t) => ({ value: t, label: `${t} (tag)` })),
  ];
  const current = String(values.gitRef ?? "").trim();
  if (current && !opts.some((o) => o.value === current)) {
    opts.unshift({ value: current, label: `${current} (current)` });
  }
  return opts;
};

// pluginPathOptions suggests the in-repo paths a plugin repository declares in
// its catalog file (evmi-plugins.json / .evmi/plugins.json), for the plugin's
// "path" combo. A repo without a catalog simply yields no suggestion — the field
// is free text, so the path can always be typed by hand.
export const pluginPathOptions = async (values: FormValues): Promise<Option[]> => {
  const gitUrl = String(values.gitUrl ?? "").trim();
  if (!gitUrl) return [];
  const r = await client.listPluginCatalog({ gitUrl, gitRef: String(values.gitRef ?? "").trim() });
  return (r.entries ?? []).map((e) => ({
    value: e.path,
    label: e.description ? `${e.name} — ${e.description}` : e.name,
  }));
};

// A single ABI entry (function/event/…) as parsed from the stored JSON.
type AbiParam = { name?: string; type: string; indexed?: boolean; components?: AbiParam[] };
type AbiItem = { type: string; name?: string; inputs?: AbiParam[] };

// One indexed argument of an event (a topics[1..] filter position).
export type IndexedArg = { name: string; type: string };

// Canonical Solidity type of a param (tuples expanded to their components), as
// used when building an event signature for keccak256.
function canonicalType(p: AbiParam): string {
  if (p.type.startsWith("tuple") && p.components) {
    return `(${p.components.map(canonicalType).join(",")})${p.type.slice("tuple".length)}`;
  }
  return p.type;
}

// Canonical event signature, e.g. "Transfer(address,address,uint256)".
function eventSignature(e: AbiItem): string {
  return `${e.name}(${(e.inputs ?? []).map(canonicalType).join(",")})`;
}

// Parse the ABI entries of the ABI whose id equals `abiId` (from the list call,
// which is guaranteed to exist). Returns [] on any miss / invalid JSON.
async function abiItems(abiId: string): Promise<AbiItem[]> {
  if (!abiId || abiId === "0") return [];
  const abi = ((await client.listEvmJsonAbis(PAGE)).abis ?? []).find((a) => String(a.id ?? 0) === abiId);
  if (!abi) return [];
  try {
    const parsed = JSON.parse(abi.content || "[]");
    return Array.isArray(parsed) ? (parsed as AbiItem[]) : [];
  } catch {
    return [];
  }
}

// Events of the ABI selected in `evmJsonAbiId`, as topic0 hashes: the option
// value is keccak256(signature) (what a TOPIC source filters on), labelled with
// the human-readable signature.
export const abiTopic0Options = async (values: FormValues): Promise<Option[]> =>
  (await abiItems(String(values.evmJsonAbiId ?? "")))
    .filter((e) => e.type === "event" && e.name)
    .map((e) => {
      const sig = eventSignature(e);
      return { value: "0x" + keccak256(sig), label: sig };
    });

// Indexed arguments (in declaration order) of the event whose topic0 hash is
// `topic0`, within the ABI `abiId`. These map one-to-one to the topics[1..]
// filter positions of a TOPIC source.
export const abiEventIndexedArgs = async (abiId: string, topic0: string): Promise<IndexedArg[]> => {
  const want = (topic0 || "").toLowerCase();
  if (!want) return [];
  const event = (await abiItems(abiId)).find(
    (e) => e.type === "event" && e.name && "0x" + keccak256(eventSignature(e)) === want,
  );
  return (event?.inputs ?? [])
    .filter((p) => p.indexed && p.name)
    .map((p) => ({ name: p.name!, type: p.type }));
};

// An event of an ABI: its name and (named) arguments.
export type AbiEvent = { name: string; inputs: IndexedArg[] };

// Events of the ABI with the given id, with their named arguments. Used by the
// factory rules editor to offer the creation event (and its address arg) as
// selects, decoded against the ABI of the factory the rule belongs to.
export const abiEvents = async (abiId: string): Promise<AbiEvent[]> =>
  (await abiItems(abiId))
    .filter((e) => e.type === "event" && e.name)
    .map((e) => ({
      name: e.name!,
      inputs: (e.inputs ?? []).filter((p) => p.name).map((p) => ({ name: p.name!, type: p.type })),
    }));

export const blockchainOptions = async (): Promise<Option[]> =>
  ((await client.listEvmBlockchains(PAGE)).blockchains ?? []).map((b) => ({
    value: String(b.id ?? 0),
    label: `${b.name} · chain ${b.chainId}`,
  }));

export const storeOptions = async (): Promise<Option[]> =>
  ((await client.listEvmLogStores(PAGE)).stores ?? []).map((s) => ({
    value: String(s.id ?? 0),
    label: `${s.identifier} (#${s.id})`,
  }));

export const pipelineOptions = async (): Promise<Option[]> =>
  ((await client.listEvmLogPipelines(PAGE)).pipelines ?? []).map((p) => ({
    value: String(p.id ?? 0),
    label: `${p.name} (#${p.id})`,
  }));

export const instanceOptions = async (): Promise<Option[]> =>
  ((await client.listEvmiInstances(PAGE)).instances ?? []).map((i) => ({
    value: String(i.id ?? 0),
    label: `${i.instanceId} (#${i.id})`,
  }));

export const pluginOptions = async (): Promise<Option[]> =>
  ((await client.listPlugins(PAGE)).plugins ?? []).map((p) => ({
    value: String(p.id ?? 0),
    label: `${p.name}${p.status === "INSTALLED" ? "" : ` · ${p.status}`}`,
  }));
