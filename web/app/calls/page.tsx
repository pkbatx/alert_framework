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

const transcriptSchema = z
  .object({
    text: z.string().optional().nullable(),
  })
  .passthrough();

const metadataSchema = z.object({}).passthrough();

const rollupSchema = z
  .object({
    title: z.string().optional().nullable(),
    summary: z.string().optional().nullable(),
    confidence: z.string().optional().nullable(),
    evidence: z.array(z.string()).optional(),
  })
  .passthrough();

type CallRow = z.infer<typeof callRowSchema>;

type CallDetail = z.infer<typeof callDetailSchema>["call"];

type TranscriptPayload = z.infer<typeof transcriptSchema>;

type MetadataPayload = z.infer<typeof metadataSchema>;

type RollupPayload = z.infer<typeof rollupSchema>;

type TimeWindow = "6" | "24" | "168";

type TabKey = "overview" | "transcript" | "metadata" | "rollup";

function formatTimestamp(value?: string | null) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function formatJson(payload: Record<string, unknown> | null) {
  if (!payload || Object.keys(payload).length === 0) {
    return "Not available.";
  }
  return JSON.stringify(payload, null, 2);
}

function highlightText(text: string, query: string) {
  if (!query.trim()) return text;
  const safe = query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const regex = new RegExp(`(${safe})`, "gi");
  const parts = text.split(regex);
  const needle = query.toLowerCase();
  return parts.map((part, idx) =>
    part.toLowerCase() === needle ? (
      <mark key={`match-${idx}`} className="rounded bg-red-500/30 px-1 text-red-100">
        {part}
      </mark>
    ) : (
      <span key={`match-${idx}`}>{part}</span>
    )
  );
}

function SkeletonRow() {
  return (
    <div className="rounded-xl border border-panelBorder bg-slate-950/50 p-3">
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
  const [windowHours, setWindowHours] = useState<TimeWindow>("24");
  const [tab, setTab] = useState<TabKey>("overview");
  const [transcriptQuery, setTranscriptQuery] = useState("");
  const [metadataView, setMetadataView] = useState<"formatted" | "json">(
    "formatted"
  );

  const fetchCalls = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await fetch(
        `/api/calls?since_hours=${windowHours}&limit=200`,
        {
          cache: "no-store",
        }
      );
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
  }, [windowHours]);

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

  const statusOptions = useMemo(() => {
    const values = new Set<string>();
    calls.forEach((call) => {
      if (call.status) values.add(call.status);
    });
    return Array.from(values.values()).sort();
  }, [calls]);

  const selectedCall = detail ?? calls.find((call) => call.id === selectedId) ?? null;

  const overviewTitle = rollup?.title || selectedCall?.filename || selectedCall?.id || "Call";
  const overviewSummary =
    rollup?.summary ||
    transcript?.text?.slice(0, 240) ||
    selectedCall?.error ||
    "No summary available.";

  const metadataObject = metadata as Record<string, unknown> | null;
  const rollupObject = rollup as Record<string, unknown> | null;

  const transcriptPresent = Boolean(transcript?.text);
  const metadataPresent = metadataObject && Object.keys(metadataObject).length > 0;
  const rollupPresent = rollupObject && Object.keys(rollupObject).length > 0;

  return (
    <div className="grid gap-6 lg:grid-cols-[360px_1fr]">
      <Card className="h-[72vh] overflow-hidden rounded-2xl">
        <CardHeader className="space-y-3">
          <div className="flex items-center justify-between">
            <CardTitle>Calls (last {windowHours}h)</CardTitle>
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
            <div className="grid grid-cols-2 gap-2">
              <select
                className="rounded-md border border-panelBorder bg-slate-900/60 px-3 py-2 text-xs"
                value={windowHours}
                onChange={(event) =>
                  setWindowHours(event.target.value as TimeWindow)
                }
              >
                <option value="6">Last 6h</option>
                <option value="24">Last 24h</option>
                <option value="168">Last 7d</option>
              </select>
              <select
                className="rounded-md border border-panelBorder bg-slate-900/60 px-3 py-2 text-xs"
                value={filters.status}
                onChange={(event) =>
                  setFilters((prev) => ({ ...prev, status: event.target.value }))
                }
              >
                <option value="">All statuses</option>
                {statusOptions.map((status) => (
                  <option key={status} value={status}>
                    {status}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex gap-2">
              <Button variant="secondary" size="sm" onClick={fetchCalls}>
                Refresh
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent className="h-[calc(72vh-178px)] overflow-y-auto pr-2">
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
            <div className="flex flex-col items-center justify-center gap-4 text-center text-xs text-slate-400">
              <img
                src="/caad-logo.svg"
                alt="CAAD logo"
                className="h-20 w-auto opacity-70"
              />
              <div>
                <p className="text-sm text-slate-200">No calls in this window.</p>
                <p className="mt-1">Drop audio into the ingest directory to populate the console.</p>
              </div>
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
                    "w-full rounded-xl border border-panelBorder bg-slate-950/50 p-3 text-left transition hover:border-accent/40",
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
                  <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-slate-400">
                    {call.source && <span>{call.source}</span>}
                    {call.status && <span>•</span>}
                    {call.status && <span>{call.status}</span>}
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
        <Card className="rounded-2xl">
          <CardHeader className="space-y-3">
            <CardTitle>Call Detail</CardTitle>
            <div className="flex flex-wrap gap-2">
              {["overview", "transcript", "metadata", "rollup"].map((key) => (
                <button
                  key={key}
                  type="button"
                  onClick={() => setTab(key as TabKey)}
                  className={cn(
                    "rounded-full border border-panelBorder px-3 py-1 text-xs",
                    tab === key
                      ? "bg-accent text-slate-950"
                      : "bg-slate-900/60 text-slate-300"
                  )}
                >
                  {key.charAt(0).toUpperCase() + key.slice(1)}
                </button>
              ))}
            </div>
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
                  <Badge variant={transcriptPresent ? "accent" : "subtle"}>
                    Transcript
                  </Badge>
                  <Badge variant={metadataPresent ? "accent" : "subtle"}>
                    Metadata
                  </Badge>
                  <Badge variant={rollupPresent ? "accent" : "subtle"}>Rollup</Badge>
                </div>

                {tab === "overview" && (
                  <div className="space-y-4">
                    <div className="rounded-xl border border-panelBorder bg-slate-950/60 p-4">
                      <p className="text-xs uppercase tracking-widest text-slate-500">Headline</p>
                      <h2 className="mt-2 text-lg font-semibold text-slate-100">
                        {overviewTitle}
                      </h2>
                      <p className="mt-2 text-sm text-slate-300">
                        {overviewSummary}
                      </p>
                    </div>
                    <div className="grid gap-3 sm:grid-cols-2">
                      <div className="rounded-xl border border-panelBorder bg-slate-950/60 p-4">
                        <p className="text-xs uppercase tracking-widest text-slate-500">Timestamp</p>
                        <p className="mt-2 text-sm text-slate-200">
                          {formatTimestamp(selectedCall.ts)}
                        </p>
                      </div>
                      {metadata && "location" in metadata && metadata.location ? (
                        <div className="rounded-xl border border-panelBorder bg-slate-950/60 p-4">
                          <p className="text-xs uppercase tracking-widest text-slate-500">Location</p>
                          <p className="mt-2 text-sm text-slate-200">
                            {String(metadata.location)}
                          </p>
                        </div>
                      ) : null}
                      {metadata && "call_type" in metadata && metadata.call_type ? (
                        <div className="rounded-xl border border-panelBorder bg-slate-950/60 p-4">
                          <p className="text-xs uppercase tracking-widest text-slate-500">Incident Type</p>
                          <p className="mt-2 text-sm text-slate-200">
                            {String(metadata.call_type)}
                          </p>
                        </div>
                      ) : null}
                      {metadata && "tags" in metadata && Array.isArray(metadata.tags) ? (
                        <div className="rounded-xl border border-panelBorder bg-slate-950/60 p-4">
                          <p className="text-xs uppercase tracking-widest text-slate-500">Tags</p>
                          <div className="mt-2 flex flex-wrap gap-2">
                            {metadata.tags.map((tag: string) => (
                              <Badge key={tag} variant="subtle">
                                {tag}
                              </Badge>
                            ))}
                          </div>
                        </div>
                      ) : null}
                      {rollup?.confidence && (
                        <div className="rounded-xl border border-panelBorder bg-slate-950/60 p-4">
                          <p className="text-xs uppercase tracking-widest text-slate-500">Confidence</p>
                          <p className="mt-2 text-sm text-slate-200">
                            {rollup.confidence}
                          </p>
                        </div>
                      )}
                    </div>
                    <div className="rounded-xl border border-panelBorder bg-slate-950/60 p-4">
                      <p className="text-xs uppercase tracking-widest text-slate-500">Audio</p>
                      {selectedCall.audio_path?.startsWith("http") ? (
                        <audio controls className="mt-3 w-full">
                          <source src={selectedCall.audio_path} />
                        </audio>
                      ) : (
                        <p className="mt-2 text-xs text-slate-400">
                          {selectedCall.audio_path || "Audio unavailable."}
                        </p>
                      )}
                    </div>
                  </div>
                )}

                {tab === "transcript" && (
                  <div className="space-y-3">
                    <input
                      className="rounded-md border border-panelBorder bg-slate-900/60 px-3 py-2 text-xs"
                      placeholder="Search transcript"
                      value={transcriptQuery}
                      onChange={(event) => setTranscriptQuery(event.target.value)}
                    />
                    <div className="rounded-xl border border-panelBorder bg-slate-950/60 p-4">
                      <p className="text-xs uppercase tracking-widest text-slate-500">
                        Transcript
                      </p>
                      <div className="mt-3 whitespace-pre-wrap text-sm text-slate-200">
                        {transcript?.text
                          ? highlightText(transcript.text, transcriptQuery)
                          : "Transcript pending."}
                      </div>
                    </div>
                  </div>
                )}

                {tab === "metadata" && (
                  <div className="space-y-3">
                    <div className="flex gap-2">
                      <Button
                        size="sm"
                        variant={metadataView === "formatted" ? "secondary" : "ghost"}
                        onClick={() => setMetadataView("formatted")}
                      >
                        Formatted
                      </Button>
                      <Button
                        size="sm"
                        variant={metadataView === "json" ? "secondary" : "ghost"}
                        onClick={() => setMetadataView("json")}
                      >
                        Raw JSON
                      </Button>
                    </div>
                    {metadataView === "formatted" ? (
                      <div className="grid gap-3 sm:grid-cols-2">
                        {metadata && "call_type" in metadata && metadata.call_type ? (
                          <div className="rounded-xl border border-panelBorder bg-slate-950/60 p-4">
                            <p className="text-xs uppercase tracking-widest text-slate-500">Call Type</p>
                            <p className="mt-2 text-sm text-slate-200">
                              {String(metadata.call_type)}
                            </p>
                          </div>
                        ) : null}
                        {metadata && "location" in metadata && metadata.location ? (
                          <div className="rounded-xl border border-panelBorder bg-slate-950/60 p-4">
                            <p className="text-xs uppercase tracking-widest text-slate-500">Location</p>
                            <p className="mt-2 text-sm text-slate-200">
                              {String(metadata.location)}
                            </p>
                          </div>
                        ) : null}
                        {metadata && "notes" in metadata && metadata.notes ? (
                          <div className="rounded-xl border border-panelBorder bg-slate-950/60 p-4 sm:col-span-2">
                            <p className="text-xs uppercase tracking-widest text-slate-500">Notes</p>
                            <p className="mt-2 text-sm text-slate-200">
                              {String(metadata.notes)}
                            </p>
                          </div>
                        ) : null}
                        {metadata && "tags" in metadata && Array.isArray(metadata.tags) ? (
                          <div className="rounded-xl border border-panelBorder bg-slate-950/60 p-4 sm:col-span-2">
                            <p className="text-xs uppercase tracking-widest text-slate-500">Tags</p>
                            <div className="mt-2 flex flex-wrap gap-2">
                              {metadata.tags.map((tag: string) => (
                                <Badge key={tag} variant="subtle">
                                  {tag}
                                </Badge>
                              ))}
                            </div>
                          </div>
                        ) : null}
                        {!metadataPresent && (
                          <div className="text-xs text-slate-400">Metadata pending.</div>
                        )}
                      </div>
                    ) : (
                      <pre className="rounded-xl border border-panelBorder bg-slate-950/60 p-4 text-xs text-slate-300">
                        {formatJson(metadataObject)}
                      </pre>
                    )}
                  </div>
                )}

                {tab === "rollup" && (
                  <div className="space-y-3">
                    <div className="rounded-xl border border-panelBorder bg-slate-950/60 p-4">
                      <p className="text-xs uppercase tracking-widest text-slate-500">Headline</p>
                      <p className="mt-2 text-lg font-semibold text-slate-100">
                        {rollup?.title || "Rollup unavailable"}
                      </p>
                      <p className="mt-2 text-sm text-slate-300">
                        {rollup?.summary || "No rollup summary available."}
                      </p>
                      {rollup?.confidence && (
                        <p className="mt-3 text-xs text-slate-400">
                          Confidence: {rollup.confidence}
                        </p>
                      )}
                      {rollup?.evidence && rollup.evidence.length > 0 && (
                        <div className="mt-3 flex flex-wrap gap-2">
                          {rollup.evidence.map((item) => (
                            <Badge key={item} variant="subtle">
                              {item}
                            </Badge>
                          ))}
                        </div>
                      )}
                    </div>
                    <details className="rounded-xl border border-panelBorder bg-slate-950/60 p-4 text-xs text-slate-300">
                      <summary className="cursor-pointer text-xs uppercase tracking-widest text-slate-500">
                        Raw JSON
                      </summary>
                      <pre className="mt-3 whitespace-pre-wrap">
                        {formatJson(rollupObject)}
                      </pre>
                    </details>
                  </div>
                )}
              </>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
