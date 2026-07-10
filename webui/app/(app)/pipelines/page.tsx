"use client";

import ResourceManager from "@/components/ResourceManager";
import { pipelines } from "@/lib/resources";

export default function Page() {
  return <ResourceManager resource={pipelines} />;
}
