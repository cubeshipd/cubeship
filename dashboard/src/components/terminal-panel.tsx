"use client";

import { cn } from "cn";
import dynamic from "next/dynamic";
import { Notice } from "@/components/notice";

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
      <Notice>
        A shell runs on a real machine, and this preview has none behind it. On an instance, this is
        a terminal inside the container.
      </Notice>
    );
  }
  return <Session path={path} query={query} password={password} className={HEIGHT} />;
}
