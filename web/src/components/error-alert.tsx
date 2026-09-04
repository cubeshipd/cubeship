import { OctagonXIcon } from "lucide-react";
import { Alert, AlertTitle } from "@/components/ui/alert";

// Every failed call in the dashboard renders through this, so a
// daemon error looks the same wherever it surfaces. Null when there is
// nothing wrong, so a caller can pass its error state straight in.
export function ErrorAlert({ error, className }: { error: string | null; className?: string }) {
  if (!error) return null;
  return (
    <Alert
      variant="destructive"
      className={
        className ?? "mb-4 border-destructive/30 border-l-2 border-l-destructive bg-destructive/8"
      }
    >
      <OctagonXIcon />
      <AlertTitle className="font-mono text-xs normal-case">{error}</AlertTitle>
    </Alert>
  );
}
