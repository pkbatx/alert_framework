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
};

function mapCall(call: UpstreamCall) {
  const timestamp = call.call_timestamp ?? call.created_at ?? "";
  return {
    id: String(call.id ?? ""),
    ts: timestamp,
    title: call.pretty_title ?? call.call_type ?? call.filename ?? "",
    summary: call.summary ?? call.clean_summary ?? "",
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

export async function GET(request: NextRequest) {
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
        `proxy /api/calls failed: ${response.status} in ${duration}ms`
      );
      return proxyJsonError("upstream error", response.status, upstream.href);
    }
    const payload = (await response.json()) as { calls?: UpstreamCall[] };
    const calls = Array.isArray(payload.calls) ? payload.calls : [];
    return NextResponse.json({ calls: calls.map(mapCall) });
  } catch (err) {
    console.warn(`proxy /api/calls error: ${(err as Error).message}`);
    return proxyJsonError("upstream unavailable", 502, upstream.href);
  }
}
