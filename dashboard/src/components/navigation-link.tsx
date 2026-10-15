"use client";

import NextLink, { useLinkStatus } from "next/link";
import type { ComponentProps } from "react";
import { createPortal } from "react-dom";

// Keep Next's prefetch, cancellation, modifier-click and history behavior.
// Feedback only appears when a navigation actually remains pending.
export default function NavigationLink({ children, ...props }: ComponentProps<typeof NextLink>) {
  return (
    <NextLink {...props}>
      {children}
      <LinkFeedback />
    </NextLink>
  );
}

function LinkFeedback() {
  const { pending } = useLinkStatus();
  return (
    <>
      <span hidden data-navigation-pending={pending || undefined} />
      <NavigationProgress pending={pending} />
    </>
  );
}

export function NavigationProgress({ pending }: { pending: boolean }) {
  if (!pending) return null;
  return createPortal(
    <div className="dashboard-navigation-progress" role="status">
      <span className="sr-only">Opening page…</span>
    </div>,
    document.body,
  );
}
