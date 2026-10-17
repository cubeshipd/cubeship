"use client";

import { TextField } from "@/components/text-field";

export function ResourceLimitFields({
  cpu,
  memory,
  onCPUChange,
  onMemoryChange,
}: {
  cpu: string;
  memory: string;
  onCPUChange: (value: string) => void;
  onMemoryChange: (value: string) => void;
}) {
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      <div className="space-y-2">
        <TextField
          label="CPU limit (cores)"
          type="number"
          step="0.01"
          min="0"
          placeholder="no limit"
          value={cpu}
          onChange={(event) => onCPUChange(event.target.value)}
        />
        <p className="text-xs text-muted-foreground">
          0 means no limit. CPU is throttled at the limit.
        </p>
      </div>
      <div className="space-y-2">
        <TextField
          label="Memory limit (MiB)"
          type="number"
          min="0"
          placeholder="no limit"
          value={memory}
          onChange={(event) => onMemoryChange(event.target.value)}
        />
        <p className="text-xs text-muted-foreground">
          0 means no limit. Exceeding it can stop the container.
        </p>
      </div>
    </div>
  );
}
