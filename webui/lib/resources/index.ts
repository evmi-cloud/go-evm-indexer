import type { Resource } from "./types";
import { blockchains } from "./blockchains";
import { abis } from "./abis";
import { stores } from "./stores";
import { pipelines } from "./pipelines";
import { sources } from "./sources";
import { plugins } from "./plugins";
import { exporters } from "./exporters";
import { accessTokens } from "./access-tokens";
import { users } from "./users";
import { oauthProviders } from "./oauth-providers";

export * from "./types";
export { blockchains, abis, stores, pipelines, sources, plugins, exporters, accessTokens, users, oauthProviders };

// All resources in setup order; used to build the navigation and routes.
// adminOnly resources are grouped separately in the nav and gated by role.
export const resources = [
  blockchains,
  abis,
  stores,
  pipelines,
  sources,
  plugins,
  exporters,
  accessTokens,
  users,
  oauthProviders,
] as Resource<any>[];

export function resourceByKey(key: string): Resource<any> | undefined {
  return resources.find((r) => r.key === key);
}
