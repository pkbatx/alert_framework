import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

const apiBase = process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:8000";
const uiSha = process.env.NEXT_PUBLIC_BUILD_SHA || "dev";
const uiTime = process.env.NEXT_PUBLIC_BUILD_TIME || "unknown";

export default function SettingsPage() {
  return (
    <div className="grid gap-6">
      <Card>
        <CardHeader>
          <CardTitle>Configuration</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm text-slate-300">
          <div>
            <p className="text-xs uppercase text-slate-500">API Base URL</p>
            <p className="font-mono text-sm">{apiBase}</p>
          </div>
          <div>
            <p className="text-xs uppercase text-slate-500">UI Build</p>
            <p className="font-mono text-sm">
              {uiSha} ({uiTime})
            </p>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Reset & Diagnostics</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm text-slate-300">
          <p>Use these commands to clear caches or restart the dev stack:</p>
          <div className="rounded-lg border border-panelBorder bg-slate-900/60 p-3 font-mono text-xs text-slate-200">
            make reset
          </div>
          <div className="rounded-lg border border-panelBorder bg-slate-900/60 p-3 font-mono text-xs text-slate-200">
            make dev
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
