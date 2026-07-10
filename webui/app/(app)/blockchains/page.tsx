"use client";

import ResourceManager from "@/components/ResourceManager";
import { blockchains } from "@/lib/resources";

export default function Page() {
  return <ResourceManager resource={blockchains} />;
}
