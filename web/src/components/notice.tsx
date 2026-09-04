import { cn } from "cn";
import { InfoIcon, TriangleAlertIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Alert, AlertDescription } from "@/components/ui/alert";

// Something true about the instance that is not an error: no domain
// yet, no autodeploy for this source, certificates not possible. The
// tone says whether it needs doing something about.
export function Notice({
  tone = "info",
  children,
  className,
}: {
  tone?: "info" | "warning";
  children: ReactNode;
  className?: string;
}) {
  const Icon = tone === "warning" ? TriangleAlertIcon : InfoIcon;
  return (
    <Alert
      className={cn(
        "mb-4 border-l-2",
        tone === "warning"
          ? "border-warning/30 border-l-warning bg-warning/6 text-warning"
          : "border-border border-l-primary bg-secondary/60",
        className,
      )}
    >
      <Icon />
      <AlertDescription
        className={tone === "warning" ? "text-warning/90" : "text-muted-foreground"}
      >
        {children}
      </AlertDescription>
    </Alert>
  );
}
