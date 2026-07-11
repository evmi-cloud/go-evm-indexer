// Async option loaders for relation (foreign-key) select fields.

import { keccak256 } from "js-sha3";
import { client } from "@/lib/client";
import { PAGE, type FormValues, type Option } from "./types";

export const abiOptions = async (): Promise<Option[]> =>
  ((await client.listEvmJsonAbis(PAGE)).abis ?? []).map((a) => ({
    value: String(a.id ?? 0),
    label: `${a.contractName} (#${a.id})`,
  }));

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

// Event names of the ABI selected in `factoryChildEvmJsonAbi`.
export const abiEventOptions = async (values: FormValues): Promise<Option[]> =>
  (await abiItems(String(values.factoryChildEvmJsonAbi ?? "")))
    .filter((e) => e.type === "event" && e.name)
    .map((e) => ({ value: e.name!, label: e.name! }));

// Arguments of the event selected in `factoryCreationFunctionName`, within the
// ABI selected in `factoryChildEvmJsonAbi`.
export const abiEventArgOptions = async (values: FormValues): Promise<Option[]> => {
  const items = await abiItems(String(values.factoryChildEvmJsonAbi ?? ""));
  const eventName = String(values.factoryCreationFunctionName ?? "");
  const event = items.find((e) => e.type === "event" && e.name === eventName);
  return (event?.inputs ?? [])
    .filter((p) => p.name)
    .map((p) => ({ value: p.name!, label: `${p.name} (${p.type})` }));
};

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
