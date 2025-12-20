"use client";

import { useEffect, useState } from "react";

import { Badge } from "@/components/ui/badge";

type Readyz = {
  ok: boolean;
  store?: boolean;
  localai?: boolean;
};

type StoreState = "unknown" | "empty" | "active";

export default function HeaderStatus() {
  const [readyz, setReadyz] = useState<Readyz | null>(null);
  const [storeState, setStoreState] = useState<StoreState>("unknown");

  useEffect(() => {
    let canceled = false;
    fetch("/api/readyz", { cache: "no-store" })
      .then((res) => res.json())
      .then((data: Readyz) => {
        if (!canceled) {
          setReadyz(data);
        }
      })
      .catch(() => {
        if (!canceled) {
          setReadyz(null);
        }
      });
    return () => {
      canceled = true;
    };
  }, []);

  useEffect(() => {
    let canceled = false;
    fetch("/api/calls?since_hours=24&limit=1", { cache: "no-store" })
      .then((res) => res.json())
      .then((data: { calls?: unknown[] }) => {
        if (!canceled) {
          const hasCalls = Array.isArray(data.calls) && data.calls.length > 0;
          setStoreState(hasCalls ? "active" : "empty");
        }
      })
      .catch(() => {
        if (!canceled) {
          setStoreState("unknown");
        }
      });
    return () => {
      canceled = true;
    };
  }, []);

  const apiOk = readyz?.ok ?? false;
  const localai = readyz?.localai;
  const store = readyz?.store;

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Badge className={apiOk ? "" : "bg-red-500/20 text-red-200 border-red-500/40"}>
        {apiOk ? "API connected" : "API down"}
      </Badge>
      <Badge
        className={
          storeState === "active"
            ? "bg-emerald-500/20 text-emerald-200 border-emerald-500/40"
            : storeState === "empty"
            ? "bg-slate-900 text-slate-400"
            : "bg-slate-900 text-slate-400"
        }
      >
        {storeState === "active"
          ? "Store active"
          : storeState === "empty"
          ? "Store empty"
          : "Store unknown"}
      </Badge>
      <Badge
        className={
          localai === true
            ? "bg-emerald-500/20 text-emerald-200 border-emerald-500/40"
            : "bg-slate-900 text-slate-400"
        }
      >
        {localai === true ? "LocalAI ready" : "LocalAI unavailable"}
      </Badge>
      {store === false && (
        <Badge className="bg-yellow-500/20 text-yellow-100 border-yellow-400/40">
          Store check failed
        </Badge>
      )}
    </div>
  );
}
