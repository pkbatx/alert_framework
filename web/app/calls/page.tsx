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
  source: z.string().optional().nullable(),
  filename: z.string().optional().nullable(),
  audio_path: z.string().optional().nullable(),
  status: z.string().optional().nullable(),
  error: z.string().optional().nullable(),
});

const callListSchema = z.object({
  calls: z.array(callRowSchema),
});

const callDetailSchema = z.object({
  call: callRowSchema,
});

const transcriptSchema = z.object({
  text: z.string().optional().nullable(),
}).passthrough();

const metadataSchema = z.object({}).passthrough();
const rollupSchema = z.object({}).passthrough();

type CallRow = z.infer<typeof callRowSchema>;

type CallDetail = z.infer<typeof callDetailSchema>["call"];
type TranscriptPayload = z.infer<typeof transcriptSchema>;
type MetadataPayload = z.infer<typeof metadataSchema>;
type RollupPayload = z.infer<typeof rollupSchema>;

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

function formatJson(payload: Record<string, unknown> | null) {
  if (!payload || Object.keys(payload).length === 0) {
    return "Not available.";
  }
  return JSON.stringify(payload, null, 2);
}

export default function CallsPage() {
  const [calls, setCalls] = useState<CallRow[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [detail, setDetail] = useState<CallDetail | null>(null);
  const [transcript, setTranscript] = useState<TranscriptPayload | null>(null);
  const [metadata, setMetadata] = useState<MetadataPayload | null>(null);
  const [rollup, setRollup] = useState<RollupPayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);
  const [filters, setFilters] = useState({
    search: "",
    status: "",
  });

  const fetchCalls = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await fetch(`/api/calls?since_hours=24&limit=200`, {
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
      setTranscript(null);
      setMetadata(null);
      setRollup(null);
      return;
    }
    setDetailLoading(true);
    setDetailError(null);
    const controller = new AbortController();
    const fetchDetail = async () => {
      try {
        const response = await fetch(`/api/calls/${selectedId}`, {
          cache: "no-store",
          signal: controller.signal,
        });
        if (!response.ok) {
          throw new Error(`status ${response.status}`);
        }
        const json = await response.json();
        const parsed = callDetailSchema.safeParse(json);
        if (parsed.success) {
          setDetail(parsed.data.call);
        } else {
          setDetail(null);
          setDetailError("Invalid detail response");
        }
      } catch (err) {
        if ((err as Error).name !== "AbortError") {
          setDetail(null);
          setDetailError("Failed to load detail");
        }
      } finally {
        setDetailLoading(false);
      }
    };

    const fetchPayload = async <T,>(
      url: string,
      schema: z.ZodType<T>,
      setter: (value: T | null) => void
    ) => {
      try {
        const response = await fetch(url, {
          cache: "no-store",
          signal: controller.signal,
        });
        if (!response.ok) {
          setter(null);
          return;
        }
        const json = await response.json();
        const parsed = schema.safeParse(json);
        setter(parsed.success ? parsed.data : null);
      } catch {
        setter(null);
      }
    };

    void fetchDetail();
    void fetchPayload(
      `/api/calls/${selectedId}/transcript`,
      transcriptSchema,
      setTranscript
    );
    void fetchPayload(
      `/api/calls/${selectedId}/metadata`,
      metadataSchema,
      setMetadata
    );
    void fetchPayload(
      `/api/calls/${selectedId}/rollup`,
      rollupSchema,
      setRollup
    );

    return () => controller.abort();
  }, [selectedId]);

  const filtered = useMemo(() => {
    const query = filters.search.trim().toLowerCase();
    return calls.filter((call) => {
      if (filters.status && call.status !== filters.status) {
        return false;
      }
      if (!query) return true;
      return [call.filename, call.source, call.status, call.error]
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
              placeholder="Search by filename, source, or status"
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
                      <Badge
                        variant={call.status === "complete" ? "accent" : "default"}
                      >
                        {call.status}
                      </Badge>
                    )}
                  </div>
                  <div className="mt-2 text-sm font-semibold text-slate-100">
                    {call.filename || call.id || "Untitled call"}
                  </div>
                  <div className="mt-1 text-xs text-slate-400">
                    {call.source || "-"}
                  </div>
                  {call.error && (
                    <div className="mt-2 line-clamp-2 text-xs text-red-300">
                      {call.error}
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
            {detailError && !detailLoading && (
              <div className="rounded-lg border border-red-500/40 bg-red-500/10 p-3 text-xs text-red-300">
                {detailError}
              </div>
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
                  {selectedCall.source && (
                    <Badge variant="subtle">{selectedCall.source}</Badge>
                  )}
                </div>
                <div className="flex flex-wrap gap-3 text-xs text-slate-400">
                  <span>ID: {selectedCall.id}</span>
                  <span>Updated: {formatTimestamp(selectedCall.ts)}</span>
                </div>
                {selectedCall.error && (
                  <div className="rounded-lg border border-red-500/40 bg-red-500/10 p-3 text-xs text-red-300">
                    {selectedCall.error}
                  </div>
                )}
                <div>
                  <p className="text-xs uppercase tracking-widest text-slate-500">
                    Audio
                  </p>
                  {selectedCall.audio_path?.startsWith("http") ? (
                    <audio controls className="w-full">
                      <source src={selectedCall.audio_path} />
                    </audio>
                  ) : (
                    <p className="mt-2 text-xs text-slate-400">
                      {selectedCall.audio_path || "No audio path available."}
                    </p>
                  )}
                </div>
                <div>
                  <p className="text-xs uppercase tracking-widest text-slate-500">
                    Transcript
                  </p>
                  <p className="mt-2 whitespace-pre-wrap text-sm text-slate-200">
                    {transcript?.text || "Transcript pending."}
                  </p>
                </div>
                <div>
                  <p className="text-xs uppercase tracking-widest text-slate-500">
                    Metadata
                  </p>
                  <pre className="mt-2 whitespace-pre-wrap text-xs text-slate-300">
                    {formatJson(metadata as Record<string, unknown> | null)}
                  </pre>
                </div>
                <div>
                  <p className="text-xs uppercase tracking-widest text-slate-500">
                    Rollup
                  </p>
                  <pre className="mt-2 whitespace-pre-wrap text-xs text-slate-300">
                    {formatJson(rollup as Record<string, unknown> | null)}
                  </pre>
                </div>
                <div className="flex flex-wrap gap-3 text-xs text-slate-400">
                  <span>File: {selectedCall.filename || "-"}</span>
                </div>
              </>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
