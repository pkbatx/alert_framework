import { NextRequest, NextResponse } from "next/server";

import { fetchUpstream, getApiBase, proxyJsonError } from "@/lib/api-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(request: NextRequest) {
  const apiBase = getApiBase();
  const upstream = new URL("/api/rollups", apiBase);
  upstream.search = request.nextUrl.search;

  try {
    const { response, duration } = await fetchUpstream(upstream);
    if (!response.ok) {
      console.warn(
        `proxy /api/rollups failed: ${response.status} in ${duration}ms`
      );
      return proxyJsonError("upstream error", response.status, upstream.href);
    }
    const payload = await response.json();
    return NextResponse.json(payload);
  } catch (err) {
    console.warn(`proxy /api/rollups error: ${(err as Error).message}`);
    return proxyJsonError("upstream unavailable", 502, upstream.href);
  }
}
