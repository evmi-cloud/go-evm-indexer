"use client";

import ResourceManager from "@/components/ResourceManager";
import { stores } from "@/lib/resources";

export default function Page() {
  return <ResourceManager resource={stores} />;
}
