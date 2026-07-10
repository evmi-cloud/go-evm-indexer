"use client";

import ResourceManager from "@/components/ResourceManager";
import { abis } from "@/lib/resources";

export default function Page() {
  return <ResourceManager resource={abis} />;
}
