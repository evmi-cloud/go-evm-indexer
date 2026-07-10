import { client } from "@/lib/client";
import type { EvmLogPipeline } from "@/gen/evm_indexer/v1/evm_indexer_pb";
import { PAGE, num, str, type Resource } from "./types";
import { instanceOptions, storeOptions } from "./options";

export const pipelines: Resource<EvmLogPipeline> = {
  key: "pipelines",
  title: "Pipelines",
  singular: "pipeline",
  fields: [
    { name: "name", label: "Name", type: "text", required: true },
    { name: "evmiInstanceId", label: "Instance", type: "select", optionsFrom: instanceOptions },
    { name: "evmLogStoreId", label: "Log store", type: "select", optionsFrom: storeOptions },
  ],
  columns: [
    { label: "ID", get: (p) => String(p.id ?? "") },
    { label: "Name", get: (p) => p.name },
    { label: "Store", get: (p) => String(p.evmLogStoreId) },
  ],
  idOf: (p) => p.id ?? 0,
  list: async () => (await client.listEvmLogPipelines(PAGE)).pipelines ?? [],
  create: async (v) => {
    await client.createEvmLogPipeline({
      pipeline: { name: str(v, "name"), evmiInstanceId: num(v, "evmiInstanceId"), evmLogStoreId: num(v, "evmLogStoreId") },
    });
  },
  update: async (id, v) => {
    await client.updateEvmLogPipeline({
      pipeline: { id, name: str(v, "name"), evmiInstanceId: num(v, "evmiInstanceId"), evmLogStoreId: num(v, "evmLogStoreId") },
    });
  },
  remove: async (id) => {
    await client.deleteEvmLogPipeline({ id });
  },
  toForm: (p) => ({
    name: p.name,
    evmiInstanceId: String(p.evmiInstanceId),
    evmLogStoreId: String(p.evmLogStoreId),
  }),
};
