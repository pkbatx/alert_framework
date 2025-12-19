"use client";

import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

type QueueStats = {
  length: number;
  capacity: number;
  workers: number;
  processed_jobs: number;
  failed_jobs: number;
};

export default function TasksPage() {
  const [stats, setStats] = useState<QueueStats | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let canceled = false;
    fetch(`/api/queue`, { cache: "no-store" })
      .then((res) => {
        if (!res.ok) {
          throw new Error(`status ${res.status}`);
        }
        return res.json();
      })
      .then((payload: QueueStats) => {
        if (!canceled) {
          setStats(payload);
          setError(null);
        }
      })
      .catch((err) => {
        if (!canceled) {
          setError(err.message || "unavailable");
        }
      });
    return () => {
      canceled = true;
    };
  }, []);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Queue Status</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm text-slate-300">
        {error && (
          <p className="text-xs text-slate-500">
            Queue unavailable ({error}).
          </p>
        )}
        {stats && (
          <div className="grid gap-2 md:grid-cols-2">
            <div>
              <p className="text-xs uppercase text-slate-500">Length</p>
              <p className="text-lg font-semibold text-white">{stats.length}</p>
            </div>
            <div>
              <p className="text-xs uppercase text-slate-500">Capacity</p>
              <p className="text-lg font-semibold text-white">{stats.capacity}</p>
            </div>
            <div>
              <p className="text-xs uppercase text-slate-500">Workers</p>
              <p className="text-lg font-semibold text-white">{stats.workers}</p>
            </div>
            <div>
              <p className="text-xs uppercase text-slate-500">Processed</p>
              <p className="text-lg font-semibold text-white">
                {stats.processed_jobs}
              </p>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
