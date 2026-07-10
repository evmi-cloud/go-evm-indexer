"use client";

import ResourceManager from "@/components/ResourceManager";
import { exporters } from "@/lib/resources";

export default function Page() {
  return <ResourceManager resource={exporters} />;
}
