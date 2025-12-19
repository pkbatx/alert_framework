"use client";

import { useEffect, useState } from "react";
import { Badge } from "@/components/ui/badge";

type Health = {
  ok: boolean;
  api: { ok: boolean; status: number };
  rollups: { ok: boolean; status: number; reason: string };
};

export default function HeaderStatus() {
  const [health, setHealth] = useState<Health | null>(null);

  useEffect(() => {
    let canceled = false;
    fetch("/api/health", { cache: "no-store" })
      .then((res) => res.json())
      .then((data: Health) => {
        if (!canceled) {
          setHealth(data);
        }
      })
      .catch(() => {
        if (!canceled) {
          setHealth(null);
        }
      });
    return () => {
      canceled = true;
    };
  }, []);

  if (!health) {
    return <Badge variant="subtle">API check pending</Badge>;
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Badge variant={health.api.ok ? "accent" : "default"}>
        {health.api.ok ? "API online" : "API down"}
      </Badge>
      <Badge variant={health.rollups.ok ? "accent" : "subtle"}>
        {health.rollups.ok ? "Rollups active" : "Rollups idle"}
      </Badge>
    </div>
  );
}
