import { cn } from "cn";
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
      // Merged, not replaced. A caller passing a margin used to lose
      // the whole house style with it — and passing `mb-0` inside a
      // `space-y` container, which every dialog here did, set the gap
      // to nothing: Tailwind's spacing rule has no specificity, so any
      // margin utility on the child wins outright. The error came out
      // welded to the first field's label.
      className={cn(
        "mb-4 border-destructive/30 border-l-2 border-l-destructive bg-destructive/8",
        className,
      )}
    >
      <OctagonXIcon />
      <AlertTitle className="font-mono text-xs normal-case">{error}</AlertTitle>
    </Alert>
  );
}
