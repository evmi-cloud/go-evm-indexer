import { client } from "@/lib/client";
import type { EvmLogStore } from "@/gen/evm_indexer/v1/evm_indexer_pb";
import { PAGE, str, type Resource } from "./types";

export const stores: Resource<EvmLogStore> = {
  key: "stores",
  title: "Log stores",
  singular: "log store",
  fields: [
    { name: "identifier", label: "Identifier", type: "text", required: true },
    { name: "description", label: "Description", type: "text" },
    {
      name: "storeType",
      label: "Store type",
      type: "select",
      options: [
        { value: "clickhouse", label: "ClickHouse" },
        { value: "parquet", label: "Parquet files" },
        { value: "elasticsearch", label: "Elasticsearch" },
      ],
    },
    {
      name: "storeConfigJson",
      label: "Store config (JSON)",
      type: "textarea",
      placeholder:
        '{ "addr": "localhost:9000", "database": "evmi_cloud", "logsTableName": "logs", "transactionsTableName": "transactions" }',
      help:
        'clickhouse: {addr, database, username, password, logsTableName, transactionsTableName} · ' +
        'parquet: {path} · ' +
        'elasticsearch: {addresses, username, password, logsIndex, transactionsIndex}',
    },
  ],
  columns: [
    { label: "ID", get: (s) => String(s.id ?? "") },
    { label: "Identifier", get: (s) => s.identifier },
    { label: "Type", get: (s) => s.storeType },
  ],
  idOf: (s) => s.id ?? 0,
  list: async () => (await client.listEvmLogStores(PAGE)).stores ?? [],
  create: async (v) => {
    await client.createEvmLogStore({
      store: {
        identifier: str(v, "identifier"),
        description: str(v, "description"),
        storeType: str(v, "storeType"),
        storeConfigJson: str(v, "storeConfigJson"),
      },
    });
  },
  update: async (id, v) => {
    await client.updateEvmLogStore({
      store: {
        id,
        identifier: str(v, "identifier"),
        description: str(v, "description"),
        storeType: str(v, "storeType"),
        storeConfigJson: str(v, "storeConfigJson"),
      },
    });
  },
  remove: async (id) => {
    await client.deleteEvmLogStore({ id });
  },
  toForm: (s) => ({
    identifier: s.identifier,
    description: s.description,
    storeType: s.storeType || "clickhouse",
    storeConfigJson: s.storeConfigJson,
  }),
};
