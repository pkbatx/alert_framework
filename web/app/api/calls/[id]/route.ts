import { NextRequest, NextResponse } from "next/server";

import { fetchUpstream, getApiBase, proxyJsonError } from "@/lib/api-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type UpstreamCall = {
  id?: number;
  filename?: string;
  status?: string;
  call_timestamp?: string;
  created_at?: string;
  pretty_title?: string;
  summary?: string;
  clean_summary?: string;
  call_type?: string;
  audio_url?: string;
  source?: string;
  location?: { label?: string };
  city_or_town?: string;
  town?: string;
  agency?: string;
  tags?: string[];
  clean_transcript_text?: string;
  transcript_text?: string;
  raw_transcript_text?: string;
};

function mapDetail(call: UpstreamCall) {
  return {
    id: String(call.id ?? ""),
    ts: call.call_timestamp ?? call.created_at ?? "",
    title: call.pretty_title ?? call.call_type ?? call.filename ?? "",
    summary: call.summary ?? call.clean_summary ?? "",
    transcript:
      call.clean_transcript_text ??
      call.transcript_text ??
      call.raw_transcript_text ??
      "",
    source: call.source ?? "",
    audio_url: call.audio_url ?? "",
    status: call.status ?? "",
    location_text:
      call.location?.label ?? call.city_or_town ?? call.town ?? "",
    filename: call.filename ?? "",
    call_type: call.call_type ?? "",
    agency: call.agency ?? "",
    tags: call.tags ?? [],
  };
}

export async function GET(
  request: NextRequest,
  context: { params: { id: string } }
) {
  const apiBase = getApiBase();
  const upstream = new URL("/api/transcriptions", apiBase);
  upstream.search = request.nextUrl.search;
  if (!upstream.searchParams.get("window")) {
    upstream.searchParams.set("window", "24h");
  }

  try {
    const { response, duration } = await fetchUpstream(upstream);
    if (!response.ok) {
      console.warn(
        `proxy /api/calls/:id failed: ${response.status} in ${duration}ms`
      );
      return proxyJsonError("upstream error", response.status, upstream.href);
    }
    const payload = (await response.json()) as { calls?: UpstreamCall[] };
    const calls = Array.isArray(payload.calls) ? payload.calls : [];
    const match = calls.find(
      (call) => String(call.id ?? "") === context.params.id
    );
    if (!match) {
      return NextResponse.json({ error: "not found" }, { status: 404 });
    }
    return NextResponse.json({ call: mapDetail(match) });
  } catch (err) {
    console.warn(`proxy /api/calls/:id error: ${(err as Error).message}`);
    return proxyJsonError("upstream unavailable", 502, upstream.href);
  }
}
