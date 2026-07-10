import type { Resource } from "./types";
import { blockchains } from "./blockchains";
import { abis } from "./abis";
import { stores } from "./stores";
import { pipelines } from "./pipelines";
import { sources } from "./sources";
import { plugins } from "./plugins";
import { exporters } from "./exporters";

export * from "./types";
export { blockchains, abis, stores, pipelines, sources, plugins, exporters };

// All resources in setup order; used to build the navigation and routes.
export const resources = [blockchains, abis, stores, pipelines, sources, plugins, exporters] as Resource<any>[];

export function resourceByKey(key: string): Resource<any> | undefined {
  return resources.find((r) => r.key === key);
}
