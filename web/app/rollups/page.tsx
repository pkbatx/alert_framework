"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { z } from "zod";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

const rollupSchema = z.object({
  rollup_id: z.number(),
  start_at: z.string(),
  end_at: z.string(),
  latitude: z.number(),
  longitude: z.number(),
  municipality: z.string().optional().nullable(),
  poi: z.string().optional().nullable(),
  category: z.string(),
  priority: z.string(),
  title: z.string().optional().nullable(),
  summary: z.string().optional().nullable(),
  evidence: z.array(z.string()).optional(),
  confidence: z.string().optional().nullable(),
  status: z.string(),
  merge_suggestion: z.string().optional().nullable(),
  model_name: z.string().optional().nullable(),
  model_base_url: z.string().optional().nullable(),
  prompt_version: z.string().optional().nullable(),
  call_count: z.number(),
  last_error: z.string().optional().nullable(),
  updated_at: z.string(),
});

const rollupListSchema = z.object({
  rollups: z.array(rollupSchema),
});

const callSchema = z.object({
  id: z.number(),
  filename: z.string(),
  status: z.string(),
  call_timestamp: z.string().optional().nullable(),
  created_at: z.string(),
  pretty_title: z.string().optional().nullable(),
  call_type: z.string().optional().nullable(),
  town: z.string().optional().nullable(),
  agency: z.string().optional().nullable(),
});

const callListSchema = z.object({
  calls: z.array(callSchema),
});

type Rollup = z.infer<typeof rollupSchema>;
type Health = {
  ok: boolean;
  api: { ok: boolean; status: number };
  rollups: { ok: boolean; status: number; reason: string };
};

type Filters = {
  from: string;
  to: string;
  status: string;
  category: string;
  priority: string;
  search: string;
};

const defaultFilters: Filters = {
  from: "",
  to: "",
  status: "",
  category: "",
  priority: "",
  search: "",
};

function formatTimestamp(value?: string | null) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

export default function RollupsPage() {
  const [rollups, setRollups] = useState<Rollup[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [filters, setFilters] = useState<Filters>(defaultFilters);
  const [calls, setCalls] = useState<z.infer<typeof callSchema>[]>([]);
  const [callsError, setCallsError] = useState<string | null>(null);
  const [health, setHealth] = useState<Health | null>(null);

  const selectedRollup = useMemo(() => {
    if (!rollups.length) return null;
    if (selectedId == null) return rollups[0];
    return rollups.find((rollup) => rollup.rollup_id === selectedId) ?? rollups[0];
  }, [rollups, selectedId]);

  const loadRollups = useCallback(async () => {
    setLoading(true);
    setError(null);
    const params = new URLSearchParams();
    if (filters.from) params.set("from", filters.from);
    if (filters.to) params.set("to", filters.to);
    if (filters.status) params.set("status", filters.status);
    params.set("limit", "200");
    try {
      const response = await fetch(`/api/rollups?${params.toString()}`, {
        cache: "no-store",
      });
      if (!response.ok) {
        setError(`status ${response.status}`);
        setLoading(false);
        return;
      }
      const json = await response.json();
      const parsed = rollupListSchema.safeParse(json);
      if (!parsed.success) {
        setError("invalid response");
        setLoading(false);
        return;
      }
      let next = parsed.data.rollups;
      if (filters.category) {
        next = next.filter((rollup) => rollup.category === filters.category);
      }
      if (filters.priority) {
        next = next.filter((rollup) => rollup.priority === filters.priority);
      }
      if (filters.search) {
        const q = filters.search.toLowerCase();
        next = next.filter((rollup) =>
          [rollup.title, rollup.summary, rollup.municipality, rollup.poi]
            .filter(Boolean)
            .some((value) => (value as string).toLowerCase().includes(q))
        );
      }
      setRollups(next);
      setSelectedId(next[0]?.rollup_id ?? null);
      setLoading(false);
    } catch (err) {
      setError((err as Error).message || "network error");
      setLoading(false);
    }
  }, [filters]);

  useEffect(() => {
    void loadRollups();
  }, [loadRollups]);

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

  useEffect(() => {
    if (!selectedRollup) {
      setCalls([]);
      return;
    }
    const fetchCalls = async () => {
      setCallsError(null);
      try {
        const response = await fetch(
          `/api/rollups/${selectedRollup.rollup_id}/calls`,
          { cache: "no-store" }
        );
        if (!response.ok) {
          setCallsError(`status ${response.status}`);
          return;
        }
        const json = await response.json();
        const parsed = callListSchema.safeParse(json);
        if (!parsed.success) {
          setCallsError("invalid response");
          return;
        }
        setCalls(parsed.data.calls);
      } catch (err) {
        setCallsError((err as Error).message || "network error");
      }
    };
    void fetchCalls();
  }, [selectedRollup]);

  return (
    <div className="grid gap-6 lg:grid-cols-[360px_1fr]">
      <Card className="h-[70vh] overflow-hidden">
        <CardHeader>
          <CardTitle>Rollups</CardTitle>
          <div className="grid gap-2 text-xs text-slate-400">
            {health?.rollups.reason === "disabled" && (
              <div className="rounded-lg border border-slate-700 bg-slate-900/70 p-2 text-xs text-slate-400">
                Rollups disabled in the API configuration.
              </div>
            )}
            {health?.rollups.reason === "unavailable" && (
              <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 p-2 text-xs text-amber-300">
                Rollups unavailable. Check worker and API connectivity.
              </div>
            )}
            {health?.rollups.reason === "api_down" && (
              <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 p-2 text-xs text-amber-300">
                API unavailable.
              </div>
            )}
            <div className="grid grid-cols-2 gap-2">
              <input
                className="rounded-md border border-panelBorder bg-slate-900/60 px-2 py-1"
                placeholder="From (RFC3339)"
                value={filters.from}
                onChange={(event) =>
                  setFilters((prev) => ({ ...prev, from: event.target.value }))
                }
              />
              <input
                className="rounded-md border border-panelBorder bg-slate-900/60 px-2 py-1"
                placeholder="To (RFC3339)"
                value={filters.to}
                onChange={(event) =>
                  setFilters((prev) => ({ ...prev, to: event.target.value }))
                }
              />
            </div>
            <div className="grid grid-cols-2 gap-2">
              <input
                className="rounded-md border border-panelBorder bg-slate-900/60 px-2 py-1"
                placeholder="Status"
                value={filters.status}
                onChange={(event) =>
                  setFilters((prev) => ({ ...prev, status: event.target.value }))
                }
              />
              <input
                className="rounded-md border border-panelBorder bg-slate-900/60 px-2 py-1"
                placeholder="Category"
                value={filters.category}
                onChange={(event) =>
                  setFilters((prev) => ({ ...prev, category: event.target.value }))
                }
              />
            </div>
            <div className="grid grid-cols-2 gap-2">
              <input
                className="rounded-md border border-panelBorder bg-slate-900/60 px-2 py-1"
                placeholder="Priority"
                value={filters.priority}
                onChange={(event) =>
                  setFilters((prev) => ({ ...prev, priority: event.target.value }))
                }
              />
              <input
                className="rounded-md border border-panelBorder bg-slate-900/60 px-2 py-1"
                placeholder="Search"
                value={filters.search}
                onChange={(event) =>
                  setFilters((prev) => ({ ...prev, search: event.target.value }))
                }
              />
            </div>
            <div className="flex gap-2">
              <Button variant="secondary" size="sm" onClick={loadRollups}>
                Apply filters
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent className="h-[calc(70vh-180px)] overflow-y-auto pr-2">
          {loading && <div className="text-xs text-slate-400">Loading…</div>}
          {error && (
            <div className="rounded-lg border border-red-500/40 bg-red-500/10 p-3 text-xs text-red-300">
              Failed to load rollups: {error}
            </div>
          )}
          {!loading && !error && rollups.length === 0 && (
            <div className="text-xs text-slate-400">No rollups yet.</div>
          )}
          <div className="space-y-3">
            {rollups.map((rollup) => {
              const isActive = selectedRollup?.rollup_id === rollup.rollup_id;
              return (
                <button
                  key={rollup.rollup_id}
                  type="button"
                  onClick={() => setSelectedId(rollup.rollup_id)}
                  className={cn(
                    "w-full rounded-lg border border-panelBorder bg-slate-950/50 p-3 text-left transition hover:border-accent/40",
                    isActive && "border-accent/80 bg-slate-900/80"
                  )}
                >
                  <div className="flex items-center justify-between">
                    <span className="text-xs text-slate-400">
                      {formatTimestamp(rollup.updated_at)}
                    </span>
                    <Badge variant={rollup.status === "LLM_OK" ? "accent" : "default"}>
                      {rollup.status}
                    </Badge>
                  </div>
                  <div className="mt-2 text-sm font-semibold text-slate-100">
                    {rollup.title || rollup.category}
                  </div>
                  <div className="mt-1 text-xs text-slate-400">
                    {[rollup.municipality, rollup.poi]
                      .filter(Boolean)
                      .join(" • ") || "-"}
                  </div>
                  <div className="mt-2 flex flex-wrap gap-2 text-xs">
                    <Badge variant="subtle">{rollup.category}</Badge>
                    <Badge variant="subtle">{rollup.priority}</Badge>
                    <Badge variant="subtle">{rollup.call_count} calls</Badge>
                  </div>
                </button>
              );
            })}
          </div>
        </CardContent>
      </Card>

      <div className="space-y-6">
        <Card>
          <CardHeader>
            <CardTitle>Rollup Detail</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {!selectedRollup && (
              <p className="text-sm text-slate-400">
                Select a rollup to view details.
              </p>
            )}
            {selectedRollup && (
              <>
                <div className="flex flex-wrap items-center gap-3">
                  <Badge variant="accent">{selectedRollup.status}</Badge>
                  <Badge>{selectedRollup.category}</Badge>
                  <Badge>{selectedRollup.priority}</Badge>
                  {selectedRollup.confidence && (
                    <Badge variant="subtle">{selectedRollup.confidence}</Badge>
                  )}
                </div>
                <div>
                  <p className="text-xs uppercase tracking-widest text-slate-500">Title</p>
                  <p className="mt-2 text-sm text-slate-200">
                    {selectedRollup.title || "Untitled rollup"}
                  </p>
                </div>
                <div>
                  <p className="text-xs uppercase tracking-widest text-slate-500">Summary</p>
                  <p className="mt-2 text-sm text-slate-200">
                    {selectedRollup.summary || "Summary pending."}
                  </p>
                </div>
                <div>
                  <p className="text-xs uppercase tracking-widest text-slate-500">Evidence</p>
                  <div className="mt-2 flex flex-wrap gap-2">
                    {selectedRollup.evidence?.length ? (
                      selectedRollup.evidence.map((item, index) => (
                        <Badge key={`${item}-${index}`} variant="subtle">
                          {item}
                        </Badge>
                      ))
                    ) : (
                      <span className="text-xs text-slate-500">No evidence yet.</span>
                    )}
                  </div>
                </div>
                <div className="grid gap-3 md:grid-cols-2">
                  <div>
                    <p className="text-xs uppercase tracking-widest text-slate-500">Geo</p>
                    <p className="mt-2 text-sm text-slate-200">
                      {selectedRollup.municipality || "-"}
                    </p>
                    <p className="text-xs text-slate-400">
                      {selectedRollup.latitude.toFixed(4)}, {selectedRollup.longitude.toFixed(4)}
                    </p>
                  </div>
                  <div>
                    <p className="text-xs uppercase tracking-widest text-slate-500">Window</p>
                    <p className="mt-2 text-sm text-slate-200">
                      {formatTimestamp(selectedRollup.start_at)} → {formatTimestamp(selectedRollup.end_at)}
                    </p>
                  </div>
                </div>
                {selectedRollup.last_error && (
                  <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-xs text-amber-300">
                    LLM error: {selectedRollup.last_error}
                  </div>
                )}
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Included Calls</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm text-slate-300">
            {callsError && (
              <p className="text-xs text-slate-500">Calls unavailable ({callsError}).</p>
            )}
            {!callsError && calls.length === 0 && (
              <p className="text-xs text-slate-500">No calls linked.</p>
            )}
            <div className="space-y-2">
              {calls.map((call) => (
                <div key={call.id} className="rounded-lg border border-panelBorder bg-slate-900/60 p-3">
                  <div className="flex items-center justify-between">
                    <span className="text-xs text-slate-400">
                      {formatTimestamp(call.call_timestamp || call.created_at)}
                    </span>
                    <Badge variant={call.status === "done" ? "accent" : "default"}>
                      {call.status}
                    </Badge>
                  </div>
                  <div className="mt-2 text-sm font-semibold text-slate-100">
                    {call.pretty_title || call.call_type || call.filename}
                  </div>
                  <div className="mt-1 text-xs text-slate-400">
                    {[call.town, call.agency].filter(Boolean).join(" • ") || "-"}
                  </div>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
