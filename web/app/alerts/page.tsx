import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export default function AlertsPage() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Alerts</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm text-slate-300">
        <p>
          Alerts appear here when the alert pipeline is enabled for this
          deployment.
        </p>
        <p className="text-xs text-slate-500">
          Rollups and clustered activity live in the Rollups view.
        </p>
      </CardContent>
    </Card>
  );
}
