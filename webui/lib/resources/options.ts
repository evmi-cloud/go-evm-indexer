// Async option loaders for relation (foreign-key) select fields.

import { client } from "@/lib/client";
import { PAGE, type Option } from "./types";

export const abiOptions = async (): Promise<Option[]> =>
  ((await client.listEvmJsonAbis(PAGE)).abis ?? []).map((a) => ({
    value: String(a.id ?? 0),
    label: `${a.contractName} (#${a.id})`,
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
