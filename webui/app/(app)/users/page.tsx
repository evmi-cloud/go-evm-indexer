"use client";

import ResourceManager from "@/components/ResourceManager";
import { users } from "@/lib/resources";

export default function Page() {
  return <ResourceManager resource={users} />;
}
