import { NextResponse } from "next/server";

import { fetchUpstream, getApiBase } from "@/lib/api-proxy";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET() {
  const apiBase = getApiBase();
  const healthURL = new URL("/healthz", apiBase);

  let apiOk = false;
  let apiStatus = 0;
  try {
    const { response } = await fetchUpstream(healthURL, {}, 4000);
    apiStatus = response.status;
    apiOk = response.ok;
  } catch {
    apiOk = false;
  }

  const rollupsOk = false;
  const rollupsStatus = 404;
  const rollupsReason = apiOk ? "disabled" : "api_down";

  return NextResponse.json({
    ok: apiOk,
    timestamp: new Date().toISOString(),
    api: { ok: apiOk, status: apiStatus },
    rollups: { ok: rollupsOk, status: rollupsStatus, reason: rollupsReason },
  });
}
