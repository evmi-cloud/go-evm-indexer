"use client";

import ResourceManager from "@/components/ResourceManager";
import { accessTokens } from "@/lib/resources";

export default function Page() {
  return <ResourceManager resource={accessTokens} />;
}
