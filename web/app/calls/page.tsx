"use client";

import { useEffect, useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

const apiBase = process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:8000";

type Call = {
  id: number;
  filename: string;
  status: string;
  call_timestamp: string;
  created_at: string;
  pretty_title?: string;
  town?: string;
  agency?: string;
  call_type?: string;
  summary?: string;
  clean_transcript_text?: string;
  transcript_text?: string;
  audio_url?: string;
  tags?: string[];
  needs_manual_review?: boolean;
};

type CallListResponse = {
  window: string;
  calls: Call[];
  stats: {
    total: number;
    status_counts: Record<string, number>;
  };
};

function formatTimestamp(value?: string) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

export default function CallsPage() {
  const [data, setData] = useState<CallListResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<number | null>(null);

  useEffect(() => {
    let canceled = false;
    fetch(`${apiBase}/api/transcriptions?window=24h`)
      .then((res) => {
        if (!res.ok) {
          throw new Error(`status ${res.status}`);
        }
        return res.json();
      })
      .then((payload: CallListResponse) => {
        if (!canceled) {
          setData(payload);
          setError(null);
        }
      })
      .catch((err) => {
        if (!canceled) {
          setError(err.message || "failed to load");
        }
      });
    return () => {
      canceled = true;
    };
  }, []);

  const calls = data?.calls ?? [];
  const selectedCall = useMemo(() => {
    if (!calls.length) return null;
    if (selectedId == null) return calls[0];
    return calls.find((call) => call.id === selectedId) ?? calls[0];
  }, [calls, selectedId]);

  return (
    <div className="grid gap-6 lg:grid-cols-[360px_1fr]">
      <Card className="h-[70vh] overflow-hidden">
        <CardHeader>
          <CardTitle>Calls (last 24h)</CardTitle>
          <p className="text-xs text-slate-400">
            {data ? `${data.stats.total} calls` : "Loading…"}
          </p>
        </CardHeader>
        <CardContent className="h-[calc(70vh-88px)] overflow-y-auto pr-2">
          {error && (
            <div className="rounded-lg border border-red-500/40 bg-red-500/10 p-3 text-xs text-red-300">
              Failed to load calls: {error}
            </div>
          )}
          {!error && calls.length === 0 && (
            <div className="text-xs text-slate-400">No calls yet.</div>
          )}
          <div className="space-y-3">
            {calls.map((call) => {
              const isActive = selectedCall?.id === call.id;
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
                  {call.needs_manual_review && (
                    <div className="mt-2 text-[11px] text-amber-300">
                      Manual review needed
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
            {!selectedCall && (
              <p className="text-sm text-slate-400">Select a call to view details.</p>
            )}
            {selectedCall && (
              <>
                <div className="flex flex-wrap items-center gap-3">
                  <Badge variant="accent">{selectedCall.status}</Badge>
                  {selectedCall.call_type && <Badge>{selectedCall.call_type}</Badge>}
                  {selectedCall.town && <Badge variant="subtle">{selectedCall.town}</Badge>}
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
                    {selectedCall.clean_transcript_text ||
                      selectedCall.transcript_text ||
                      "Transcript pending."}
                  </p>
                </div>
                <div className="flex flex-wrap gap-3 text-xs text-slate-400">
                  <span>File: {selectedCall.filename}</span>
                  <span>Updated: {formatTimestamp(selectedCall.created_at)}</span>
                </div>
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Quick Actions</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-wrap gap-2">
            <Button variant="secondary" size="sm" disabled>
              Re-run transcribe
            </Button>
            <Button variant="secondary" size="sm" disabled>
              Enrich metadata
            </Button>
            <Button variant="secondary" size="sm" disabled>
              Send alert
            </Button>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
