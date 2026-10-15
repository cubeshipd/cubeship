"use client";

import { useEffect } from "react";
import { eventData } from "@/lib/click-events";

declare global {
  interface Window {
    umami?: { track(event: string, data?: Record<string, string>): void };
  }
}

// A click on anything carrying data-track is sent to Umami as that event,
// with each data-track-<name> attribute as a property. Not Umami's own
// data-umami-event: on a link, its tracker cancels the click and sets
// location.href once the event is sent, turning every client-side Link
// into a full page load.
export function ClickTracking() {
  useEffect(() => {
    function onClick(event: MouseEvent) {
      const element = event.target instanceof Element ? event.target.closest("[data-track]") : null;
      if (!(element instanceof HTMLElement)) return;
      const tracked = eventData(element.dataset);
      if (tracked) window.umami?.track(tracked.name, tracked.data);
    }
    document.addEventListener("click", onClick, { capture: true });
    return () => document.removeEventListener("click", onClick, { capture: true });
  }, []);
  return null;
}
