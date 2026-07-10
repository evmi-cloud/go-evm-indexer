"use client";

import ResourceManager from "@/components/ResourceManager";
import { oauthProviders } from "@/lib/resources";

export default function Page() {
  return <ResourceManager resource={oauthProviders} />;
}
