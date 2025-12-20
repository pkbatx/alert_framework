import { NextRequest, NextResponse } from "next/server";

import { fetchUpstream, getApiBase, proxyJsonError } from "@/lib/api-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type UpstreamCall = {
  id?: string;
  ts?: string;
  source?: string;
  original_filename?: string;
  stored_audio_path?: string;
  status?: string;
  error?: string;
};

function mapCall(call: UpstreamCall) {
  return {
    id: String(call.id ?? ""),
    ts: call.ts ?? "",
    source: call.source ?? "",
    filename: call.original_filename ?? "",
    audio_path: call.stored_audio_path ?? "",
    status: call.status ?? "",
    error: call.error ?? "",
  };
}

export async function GET(request: NextRequest) {
  const apiBase = getApiBase();
  const upstream = new URL("/calls", apiBase);
  const params = request.nextUrl.searchParams;
  const sinceHours = params.get("since_hours") ?? params.get("sinceHours");
  const windowParam = params.get("window");
  if (sinceHours) {
    upstream.searchParams.set("since_hours", sinceHours);
  } else if (windowParam?.endsWith("h")) {
    upstream.searchParams.set("since_hours", windowParam.replace("h", ""));
  } else {
    upstream.searchParams.set("since_hours", "24");
  }
  upstream.searchParams.set("limit", params.get("limit") ?? "200");

  try {
    const { response, duration } = await fetchUpstream(upstream);
    if (!response.ok) {
      console.warn(
        `proxy /api/calls failed: ${response.status} in ${duration}ms`
      );
      return proxyJsonError("upstream error", response.status, upstream.href);
    }
    const payload = (await response.json()) as UpstreamCall[];
    const calls = Array.isArray(payload) ? payload : [];
    return NextResponse.json({ calls: calls.map(mapCall) });
  } catch (err) {
    console.warn(`proxy /api/calls error: ${(err as Error).message}`);
    return proxyJsonError("upstream unavailable", 502, upstream.href);
  }
}
