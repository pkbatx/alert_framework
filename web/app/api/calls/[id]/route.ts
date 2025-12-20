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

function mapDetail(call: UpstreamCall) {
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

export async function GET(
  request: NextRequest,
  context: { params: { id: string } }
) {
  const apiBase = getApiBase();
  const upstream = new URL(`/calls/${context.params.id}`, apiBase);

  try {
    const { response, duration } = await fetchUpstream(upstream);
    if (!response.ok) {
      console.warn(
        `proxy /api/calls/:id failed: ${response.status} in ${duration}ms`
      );
      return proxyJsonError("upstream error", response.status, upstream.href);
    }
    const payload = (await response.json()) as UpstreamCall;
    return NextResponse.json({ call: mapDetail(payload) });
  } catch (err) {
    console.warn(`proxy /api/calls/:id error: ${(err as Error).message}`);
    return proxyJsonError("upstream unavailable", 502, upstream.href);
  }
}
