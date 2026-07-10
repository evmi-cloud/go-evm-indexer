"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { tokenStore } from "@/lib/client";
import { resources } from "@/lib/resources";

// Entry route: send the user to the first resource or to login.
export default function Index() {
  const router = useRouter();
  useEffect(() => {
    router.replace(tokenStore.get() ? `/${resources[0].key}` : "/login");
  }, [router]);
  return <main />;
}
