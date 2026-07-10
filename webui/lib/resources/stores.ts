import { client } from "@/lib/client";
import type { EvmLogStore } from "@/gen/evm_indexer/v1/evm_indexer_pb";
import { PAGE, str, type ConfigParam, type Resource } from "./types";

// Config parameters per store type, driving the dynamic form.
const storeConfigSchemas: Record<string, ConfigParam[]> = {
  clickhouse: [
    { name: "addr", placeholder: "localhost:9000", help: "Comma-separated for multiple nodes", required: true },
    { name: "database", placeholder: "evmi_cloud", required: true },
    { name: "username", placeholder: "default" },
    { name: "password", type: "password" },
    { name: "logsTableName", placeholder: "logs" },
    { name: "transactionsTableName", placeholder: "transactions" },
  ],
  parquet: [{ name: "path", placeholder: "/data/parquet", help: "Base directory for the .parquet files", required: true }],
  elasticsearch: [
    { name: "addresses", placeholder: "http://localhost:9200", help: "Comma-separated for multiple nodes", required: true },
    { name: "username" },
    { name: "password", type: "password" },
    { name: "logsIndex", placeholder: "evmi_logs" },
    { name: "transactionsIndex", placeholder: "evmi_transactions" },
  ],
  postgres: [
    { name: "dsn", placeholder: "host=localhost user=evmi password=secret dbname=evmi port=5432 sslmode=disable", required: true },
  ],
  mysql: [{ name: "dsn", placeholder: "user:pass@tcp(localhost:3306)/evmi?parseTime=true", required: true }],
  mongodb: [
    { name: "uri", placeholder: "mongodb://localhost:27017", required: true },
    { name: "database", placeholder: "evmi" },
    { name: "logsCollection", placeholder: "logs" },
    { name: "transactionsCollection", placeholder: "transactions" },
  ],
};

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
        { value: "postgres", label: "PostgreSQL" },
        { value: "mysql", label: "MySQL" },
        { value: "mongodb", label: "MongoDB" },
      ],
    },
    {
      name: "storeConfigJson",
      label: "Store config",
      type: "keyedConfig",
      dependsOn: "storeType",
      schemas: storeConfigSchemas,
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
