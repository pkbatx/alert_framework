"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { z } from "zod";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

const callRowSchema = z.object({
  id: z.string(),
  ts: z.string(),
  title: z.string().optional().nullable(),
  summary: z.string().optional().nullable(),
  transcript: z.string().optional().nullable(),
  source: z.string().optional().nullable(),
  audio_url: z.string().optional().nullable(),
  status: z.string().optional().nullable(),
  location_text: z.string().optional().nullable(),
  filename: z.string().optional().nullable(),
  call_type: z.string().optional().nullable(),
  agency: z.string().optional().nullable(),
  tags: z.array(z.string()).optional(),
});

const callListSchema = z.object({
  calls: z.array(callRowSchema),
});

const callDetailSchema = z.object({
  call: z.object({
    id: z.string(),
    ts: z.string(),
    title: z.string().optional().nullable(),
    summary: z.string().optional().nullable(),
    transcript: z.string().optional().nullable(),
    source: z.string().optional().nullable(),
    audio_url: z.string().optional().nullable(),
    status: z.string().optional().nullable(),
    location_text: z.string().optional().nullable(),
    filename: z.string().optional().nullable(),
    call_type: z.string().optional().nullable(),
    agency: z.string().optional().nullable(),
    tags: z.array(z.string()).optional(),
  }),
});

type CallRow = z.infer<typeof callRowSchema>;

type CallDetail = z.infer<typeof callDetailSchema>["call"];

function formatTimestamp(value?: string | null) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function SkeletonRow() {
  return (
    <div className="rounded-lg border border-panelBorder bg-slate-950/50 p-3">
      <div className="h-3 w-24 animate-pulse rounded bg-slate-800" />
      <div className="mt-3 h-4 w-48 animate-pulse rounded bg-slate-800" />
      <div className="mt-2 h-3 w-32 animate-pulse rounded bg-slate-800" />
    </div>
  );
}

export default function CallsPage() {
  const [calls, setCalls] = useState<CallRow[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [detail, setDetail] = useState<CallDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [filters, setFilters] = useState({
    search: "",
    status: "",
  });

  const fetchCalls = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await fetch(`/api/calls?window=24h`, {
        cache: "no-store",
      });
      if (!response.ok) {
        setError(`Upstream error (${response.status})`);
        setLoading(false);
        return;
      }
      const json = await response.json();
      const parsed = callListSchema.safeParse(json);
      if (!parsed.success) {
        setError("Invalid response from API");
        setLoading(false);
        return;
      }
      setCalls(parsed.data.calls);
      setSelectedId(parsed.data.calls[0]?.id ?? null);
      setLoading(false);
    } catch (err) {
      setError((err as Error).message || "Network error");
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchCalls();
  }, [fetchCalls]);

  useEffect(() => {
    if (!selectedId) {
      setDetail(null);
      return;
    }
    setDetailLoading(true);
    fetch(`/api/calls/${selectedId}?window=24h`, { cache: "no-store" })
      .then((res) => {
        if (!res.ok) {
          throw new Error(`status ${res.status}`);
        }
        return res.json();
      })
      .then((json) => {
        const parsed = callDetailSchema.safeParse(json);
        if (parsed.success) {
          setDetail(parsed.data.call);
        } else {
          setDetail(null);
        }
      })
      .catch(() => {
        setDetail(null);
      })
      .finally(() => setDetailLoading(false));
  }, [selectedId]);

  const filtered = useMemo(() => {
    const query = filters.search.trim().toLowerCase();
    return calls.filter((call) => {
      if (filters.status && call.status !== filters.status) {
        return false;
      }
      if (!query) return true;
      return [call.title, call.summary, call.location_text, call.filename]
        .filter(Boolean)
        .some((value) => (value as string).toLowerCase().includes(query));
    });
  }, [calls, filters]);

  const selectedCall = detail ?? calls.find((call) => call.id === selectedId) ?? null;

  return (
    <div className="grid gap-6 lg:grid-cols-[360px_1fr]">
      <Card className="h-[70vh] overflow-hidden">
        <CardHeader className="space-y-3">
          <div className="flex items-center justify-between">
            <CardTitle>Calls (last 24h)</CardTitle>
            <Badge variant="subtle">{filtered.length} calls</Badge>
          </div>
          <div className="grid gap-2">
            <input
              className="rounded-md border border-panelBorder bg-slate-900/60 px-3 py-2 text-xs"
              placeholder="Search by title, location, or summary"
              value={filters.search}
              onChange={(event) =>
                setFilters((prev) => ({ ...prev, search: event.target.value }))
              }
            />
            <input
              className="rounded-md border border-panelBorder bg-slate-900/60 px-3 py-2 text-xs"
              placeholder="Status filter"
              value={filters.status}
              onChange={(event) =>
                setFilters((prev) => ({ ...prev, status: event.target.value }))
              }
            />
            <div className="flex gap-2">
              <Button variant="secondary" size="sm" onClick={fetchCalls}>
                Refresh
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent className="h-[calc(70vh-178px)] overflow-y-auto pr-2">
          {error && (
            <div className="rounded-lg border border-red-500/40 bg-red-500/10 p-3 text-xs text-red-300">
              Failed to load calls. {error}
              <div className="mt-2">
                <Button variant="ghost" size="sm" onClick={fetchCalls}>
                  Retry
                </Button>
              </div>
            </div>
          )}
          {loading && (
            <div className="space-y-3">
              {Array.from({ length: 6 }).map((_, index) => (
                <SkeletonRow key={`skeleton-${index}`} />
              ))}
            </div>
          )}
          {!loading && !error && filtered.length === 0 && (
            <div className="text-xs text-slate-400">
              No calls in the last 24h.
            </div>
          )}
          <div className="space-y-3">
            {filtered.map((call) => {
              const isActive = selectedId === call.id;
              return (
                <button
                  key={call.id}
                  type="button"
                  onClick={() => setSelectedId(call.id)}
                  className={cn(
                    "w-full rounded-lg border border-panelBorder bg-slate-950/50 p-3 text-left transition hover:border-accent/40",
                    isActive && "border-accent/80 bg-slate-900/80"
                  )}
                >
                  <div className="flex items-center justify-between">
                    <span className="text-xs text-slate-400">
                      {formatTimestamp(call.ts)}
                    </span>
                    {call.status && (
                      <Badge variant={call.status === "done" ? "accent" : "default"}>
                        {call.status}
                      </Badge>
                    )}
                  </div>
                  <div className="mt-2 text-sm font-semibold text-slate-100">
                    {call.title || call.filename || "Untitled call"}
                  </div>
                  <div className="mt-1 text-xs text-slate-400">
                    {call.location_text || call.agency || "-"}
                  </div>
                  {call.summary && (
                    <div className="mt-2 line-clamp-2 text-xs text-slate-400">
                      {call.summary}
                    </div>
                  )}
                </button>
              );
            })}
          </div>
        </CardContent>
      </Card>

      <div className="space-y-6">
        <Card>
          <CardHeader>
            <CardTitle>Call Detail</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {!selectedCall && !detailLoading && (
              <p className="text-sm text-slate-400">Select a call to view details.</p>
            )}
            {detailLoading && (
              <div className="space-y-3">
                <div className="h-4 w-40 animate-pulse rounded bg-slate-800" />
                <div className="h-24 w-full animate-pulse rounded bg-slate-800" />
              </div>
            )}
            {selectedCall && !detailLoading && (
              <>
                <div className="flex flex-wrap items-center gap-3">
                  {selectedCall.status && (
                    <Badge variant="accent">{selectedCall.status}</Badge>
                  )}
                  {selectedCall.call_type && (
                    <Badge>{selectedCall.call_type}</Badge>
                  )}
                  {selectedCall.location_text && (
                    <Badge variant="subtle">{selectedCall.location_text}</Badge>
                  )}
                  {selectedCall.tags?.map((tag) => (
                    <Badge key={tag} variant="subtle">
                      {tag}
                    </Badge>
                  ))}
                </div>
                {selectedCall.audio_url ? (
                  <audio controls className="w-full">
                    <source src={selectedCall.audio_url} />
                  </audio>
                ) : (
                  <p className="text-xs text-slate-500">No audio URL available.</p>
                )}
                <div>
                  <p className="text-xs uppercase tracking-widest text-slate-500">Summary</p>
                  <p className="mt-2 text-sm text-slate-200">
                    {selectedCall.summary || "No summary available."}
                  </p>
                </div>
                <div>
                  <p className="text-xs uppercase tracking-widest text-slate-500">Transcript</p>
                  <p className="mt-2 whitespace-pre-wrap text-sm text-slate-200">
                    {selectedCall.transcript || "Transcript pending."}
                  </p>
                </div>
                <div className="flex flex-wrap gap-3 text-xs text-slate-400">
                  <span>File: {selectedCall.filename || "-"}</span>
                  <span>Updated: {formatTimestamp(selectedCall.ts)}</span>
                </div>
              </>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
