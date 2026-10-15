"use client";

import { Shell } from "@/components/shell";

// The route group exists so the Shell is a layout rather than something
// each page renders.
//
// It used to be inside every page, and that is what made navigation
// flash: moving from one page to another unmounted the whole chrome —
// sidebar included — remounted it, refetched who you are, and rendered
// nothing while that was in flight. A layout is mounted once and
// survives every navigation under it.
//
// The group's name is in parentheses, so it shapes the tree without
// appearing in any URL: /apps stays /apps.
export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  return <Shell>{children}</Shell>;
}
