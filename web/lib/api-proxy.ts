import { NextResponse } from "next/server";

const DEFAULT_TIMEOUT_MS = 6000;

export function getApiBase() {
  const explicit = process.env.API_BASE_URL?.trim();
  if (explicit) {
    return explicit;
  }
  const inDocker =
    process.env.IN_DOCKER === "true" || process.env.DOCKER === "true";
  return inDocker ? "http://api:8000" : "http://localhost:8000";
}

export async function fetchUpstream(
  url: URL,
  init: RequestInit = {},
  timeoutMs = DEFAULT_TIMEOUT_MS
) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  const started = Date.now();
  try {
    const response = await fetch(url, {
      ...init,
      signal: controller.signal,
      cache: "no-store",
      headers: {
        ...(init.headers ?? {}),
      },
    });
    const duration = Date.now() - started;
    return { response, duration };
  } finally {
    clearTimeout(timeout);
  }
}

export async function proxyJsonError(
  message: string,
  status: number,
  upstream?: string
) {
  return NextResponse.json(
    {
      error: message,
      status,
      upstream,
    },
    { status }
  );
}
