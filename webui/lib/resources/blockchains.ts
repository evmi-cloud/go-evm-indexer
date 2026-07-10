import { client } from "@/lib/client";
import type { EvmBlockchain } from "@/gen/evm_indexer/v1/evm_indexer_pb";
import { PAGE, big, str, type Resource } from "./types";

export const blockchains: Resource<EvmBlockchain> = {
  key: "blockchains",
  title: "Blockchains",
  singular: "blockchain",
  fields: [
    { name: "name", label: "Name", type: "text", required: true },
    { name: "chainId", label: "Chain ID", type: "bigint", required: true },
    { name: "rpcUrl", label: "RPC URL", type: "text", required: true },
    { name: "blockRange", label: "Block range", type: "bigint", placeholder: "1000" },
    { name: "blockSlice", label: "Block slice (confirmations)", type: "bigint", placeholder: "4" },
    { name: "pullInterval", label: "Pull interval (s)", type: "bigint", placeholder: "2" },
    { name: "rpcMaxBatchSize", label: "RPC max batch size", type: "bigint", placeholder: "1000" },
  ],
  columns: [
    { label: "ID", get: (b) => String(b.id ?? "") },
    { label: "Name", get: (b) => b.name },
    { label: "Chain", get: (b) => String(b.chainId), mono: true },
    { label: "RPC", get: (b) => b.rpcUrl, mono: true },
  ],
  idOf: (b) => b.id ?? 0,
  list: async () => (await client.listEvmBlockchains(PAGE)).blockchains ?? [],
  create: async (v) => {
    await client.createEvmBlockchain({
      blockchain: {
        name: str(v, "name"),
        chainId: big(v, "chainId"),
        rpcUrl: str(v, "rpcUrl"),
        blockRange: big(v, "blockRange"),
        blockSlice: big(v, "blockSlice"),
        pullInterval: big(v, "pullInterval"),
        rpcMaxBatchSize: big(v, "rpcMaxBatchSize"),
      },
    });
  },
  update: async (id, v) => {
    await client.updateEvmBlockchain({
      blockchain: {
        id,
        name: str(v, "name"),
        chainId: big(v, "chainId"),
        rpcUrl: str(v, "rpcUrl"),
        blockRange: big(v, "blockRange"),
        blockSlice: big(v, "blockSlice"),
        pullInterval: big(v, "pullInterval"),
        rpcMaxBatchSize: big(v, "rpcMaxBatchSize"),
      },
    });
  },
  remove: async (id) => {
    await client.deleteEvmBlockchain({ id });
  },
  toForm: (b) => ({
    name: b.name,
    chainId: String(b.chainId),
    rpcUrl: b.rpcUrl,
    blockRange: String(b.blockRange),
    blockSlice: String(b.blockSlice),
    pullInterval: String(b.pullInterval),
    rpcMaxBatchSize: String(b.rpcMaxBatchSize),
  }),
};
