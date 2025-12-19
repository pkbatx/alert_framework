import { NextResponse } from "next/server";

import { fetchUpstream, getApiBase } from "@/lib/api-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET() {
  const apiBase = getApiBase();
  const healthURL = new URL("/healthz", apiBase);
  const rollupsURL = new URL("/api/rollups", apiBase);
  rollupsURL.searchParams.set("limit", "1");

  let apiOk = false;
  let apiStatus = 0;
  try {
    const { response } = await fetchUpstream(healthURL, {}, 4000);
    apiStatus = response.status;
    apiOk = response.ok;
  } catch {
    apiOk = false;
  }

  let rollupsOk = false;
  let rollupsStatus = 0;
  let rollupsReason = "unknown";
  if (apiOk) {
    try {
      const { response } = await fetchUpstream(rollupsURL, {}, 4000);
      rollupsStatus = response.status;
      rollupsOk = response.ok;
      if (!rollupsOk && rollupsStatus === 404) {
        rollupsReason = "disabled";
      } else if (!rollupsOk) {
        rollupsReason = "unavailable";
      } else {
        rollupsReason = "ok";
      }
    } catch {
      rollupsOk = false;
      rollupsReason = "unavailable";
    }
  } else {
    rollupsReason = "api_down";
  }

  return NextResponse.json({
    ok: apiOk,
    timestamp: new Date().toISOString(),
    api: { ok: apiOk, status: apiStatus },
    rollups: { ok: rollupsOk, status: rollupsStatus, reason: rollupsReason },
  });
}
