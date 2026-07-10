"use client";

import ResourceManager from "@/components/ResourceManager";
import { plugins } from "@/lib/resources";

export default function Page() {
  return <ResourceManager resource={plugins} />;
}
