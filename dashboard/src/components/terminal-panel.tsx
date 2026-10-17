"use client";

import { cn } from "cn";
import dynamic from "next/dynamic";
import { useState } from "react";
import { SearchableSelect } from "@/components/searchable-select";

// The size a terminal holds before it has loaded, which is the size it
// has after. A panel that grew into its space would push everything
// under it down the page.
const HEIGHT = "h-[min(70vh,44rem)] min-h-80";

const Session = dynamic(() => import("@/components/terminal-session"), {
  ssr: false,
  loading: () => (
    <div className="flex flex-col gap-2">
      <div className="h-8" />
      <div className={cn("border border-border bg-black", HEIGHT)} />
    </div>
  ),
});

// PREVIEW is the dashboard with no daemon behind it, where there is
// nothing to open a shell on.
const PREVIEW = process.env.NEXT_PUBLIC_CUBESHIP_MOCK === "1";

export function TerminalPanel({
  path,
  query,
  password,
}: {
  path: string;
  query?: Record<string, string>;
  password?: boolean;
}) {
  if (PREVIEW) {
    return (
      <div className="space-y-2">
        <p className="font-mono text-[11px] tracking-wide text-muted-foreground">
          SIMULATED · SAMPLE OUTPUT ONLY
        </p>
        <pre
          role="img"
          aria-label="Simulated terminal preview"
          className={cn(
            "overflow-auto border border-border bg-black p-4 font-mono text-[13px] leading-relaxed text-(--ansi-white)",
            HEIGHT,
          )}
        >
          <span className="text-(--ansi-green)">app@api</span>:
          <span className="text-(--ansi-blue)">/app</span>$ pwd{"\n"}
          /app{"\n"}
          <span className="text-(--ansi-green)">app@api</span>:
          <span className="text-(--ansi-blue)">/app</span>$ ls{"\n"}
          package.json src dist{"\n"}
          <span className="text-(--ansi-green)">app@api</span>:
          <span className="text-(--ansi-blue)">/app</span>$ node --version{"\n"}
          v22.14.0{"\n"}
          <span className="text-(--ansi-green)">app@api</span>:
          <span className="text-(--ansi-blue)">/app</span>$ <span className="animate-pulse">█</span>
        </pre>
      </div>
    );
  }
  return <Session path={path} query={query} password={password} className={HEIGHT} />;
}

export function AppShell({ path, servers }: { path: string; servers: string[] }) {
  const [server, setServer] = useState(servers[0] ?? "");
  return (
    <div className="space-y-3">
      {servers.length > 1 && (
        <SearchableSelect
          label="Machine"
          value={server}
          onChange={setServer}
          choices={servers.map((s) => ({ value: s, label: s }))}
          searchable={false}
          fieldClassName="max-w-xs"
        />
      )}
      <TerminalPanel
        key={server}
        path={`${path}/shell`}
        query={servers.length > 1 ? { server } : undefined}
      />
    </div>
  );
}
