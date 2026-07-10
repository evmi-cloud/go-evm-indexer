import { client } from "@/lib/client";
import type { EvmJsonAbi } from "@/gen/evm_indexer/v1/evm_indexer_pb";
import AbiDetail from "@/components/AbiDetail";
import { PAGE, str, type Resource } from "./types";

export const abis: Resource<EvmJsonAbi> = {
  key: "abis",
  title: "ABIs",
  singular: "ABI",
  fields: [
    { name: "contractName", label: "Contract name", type: "text", required: true },
    { name: "content", label: "ABI JSON", type: "textarea", required: true, placeholder: "[ { ... } ]" },
  ],
  columns: [
    { label: "ID", get: (a) => String(a.id ?? "") },
    { label: "Contract", get: (a) => a.contractName },
    { label: "Entries", get: (a) => String(countAbiEntries(a.content)) },
  ],
  detail: AbiDetail,
  idOf: (a) => a.id ?? 0,
  list: async () => (await client.listEvmJsonAbis(PAGE)).abis ?? [],
  create: async (v) => {
    await client.createEvmJsonAbi({ abi: { contractName: str(v, "contractName"), content: str(v, "content") } });
  },
  update: async (id, v) => {
    await client.updateEvmJsonAbi({ abi: { id, contractName: str(v, "contractName"), content: str(v, "content") } });
  },
  remove: async (id) => {
    await client.deleteEvmJsonAbi({ id });
  },
  toForm: (a) => ({ contractName: a.contractName, content: a.content }),
};

function countAbiEntries(content: string): number {
  try {
    const abi = JSON.parse(content || "[]");
    return Array.isArray(abi) ? abi.filter((e) => e.type === "function" || e.type === "event").length : 0;
  } catch {
    return 0;
  }
}
