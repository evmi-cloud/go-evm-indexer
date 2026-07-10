"use client";

import ResourceManager from "@/components/ResourceManager";
import { sources } from "@/lib/resources";

export default function Page() {
  return <ResourceManager resource={sources} />;
}
