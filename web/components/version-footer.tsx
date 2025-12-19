"use client";

import { useEffect, useState } from "react";

type VersionInfo = {
  version: string;
  git_sha: string;
  build_time: string;
};

const uiSha = process.env.NEXT_PUBLIC_BUILD_SHA || "dev";
const uiTime = process.env.NEXT_PUBLIC_BUILD_TIME || "unknown";

export default function VersionFooter() {
  const [apiVersion, setApiVersion] = useState<VersionInfo | null>(null);
  const [apiError, setApiError] = useState<string | null>(null);

  useEffect(() => {
    let canceled = false;
    fetch(`/api/version`, { cache: "no-store" })
      .then((res) => {
        if (!res.ok) {
          throw new Error(`status ${res.status}`);
        }
        return res.json();
      })
      .then((data: VersionInfo) => {
        if (!canceled) {
          setApiVersion(data);
          setApiError(null);
        }
      })
      .catch((err) => {
        if (!canceled) {
          setApiError(err.message || "unavailable");
        }
      });
    return () => {
      canceled = true;
    };
  }, []);

  return (
    <footer className="mt-8 border-t border-panelBorder bg-slate-950/60 px-6 py-4 text-xs text-slate-400">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span>
          UI build: {uiSha} ({uiTime})
        </span>
        <span>
          API build:{" "}
          {apiVersion
            ? `${apiVersion.version} ${apiVersion.git_sha} (${apiVersion.build_time})`
            : apiError
              ? `unavailable (${apiError})`
              : "loading"}
        </span>
      </div>
    </footer>
  );
}
