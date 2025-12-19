import { NextResponse } from "next/server";

import { fetchUpstream, getApiBase, proxyJsonError } from "@/lib/api-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET() {
  const apiBase = getApiBase();
  const upstream = new URL("/api/version", apiBase);

  try {
    const { response, duration } = await fetchUpstream(upstream);
    if (!response.ok) {
      console.warn(
        `proxy /api/version failed: ${response.status} in ${duration}ms`
      );
      return proxyJsonError("upstream error", response.status, upstream.href);
    }
    const payload = await response.json();
    return NextResponse.json(payload);
  } catch (err) {
    console.warn(`proxy /api/version error: ${(err as Error).message}`);
    return proxyJsonError("upstream unavailable", 502, upstream.href);
  }
}
