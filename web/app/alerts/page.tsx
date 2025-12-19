import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export default function AlertsPage() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Alerts</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm text-slate-300">
        <p>Alert rollups will appear here once the alert pipeline is enabled.</p>
        <p className="text-xs text-slate-500">
          This console currently reads from the existing transcriptions API.
        </p>
      </CardContent>
    </Card>
  );
}
