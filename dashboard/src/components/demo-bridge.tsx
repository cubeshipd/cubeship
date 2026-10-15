"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";
import { toast } from "sonner";

// The parent can select a tour stop, but cannot send arbitrary URLs or actions.
const stops: Record<string, string> = {
  overview: "/",
  app: "/projects/web/production/api",
  backups: "/backups",
  projects: "/projects",
  components: "/components",
};

export function DemoBridge() {
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    const receive = (event: MessageEvent) => {
      if (event.source !== window.parent || event.origin !== window.location.origin) return;
      if (event.data?.type !== "cubeship:demo:navigate") return;
      if (typeof event.data.screen !== "string" || !Object.hasOwn(stops, event.data.screen)) return;
      const route = stops[event.data.screen];
      if (route) router.push(route);
    };
    const keepInDemo = (event: MouseEvent) => {
      const anchor = (event.target as Element)?.closest?.("a[href]");
      if (!(anchor instanceof HTMLAnchorElement)) return;
      const url = new URL(anchor.href);
      if (url.protocol === "blob:" || anchor.hasAttribute("download")) return;
      if (
        url.origin === window.location.origin &&
        (url.pathname === "/demo" || url.pathname.startsWith("/demo/"))
      )
        return;
      event.preventDefault();
      event.stopPropagation();
      toast.info(
        "This link belongs to the example infrastructure. Explore the dashboard here, or install Cubeship to connect your own.",
      );
    };
    const stopExternalForm = (event: SubmitEvent) => {
      const form = event.target;
      if (!(form instanceof HTMLFormElement) || !form.getAttribute("action")) return;
      if (new URL(form.action).origin === window.location.origin) return;
      event.preventDefault();
      event.stopPropagation();
      toast.info(
        "External services can be connected on your own Cubeship instance. This demo uses sample connections.",
      );
    };
    window.addEventListener("message", receive);
    document.addEventListener("click", keepInDemo, true);
    document.addEventListener("submit", stopExternalForm, true);
    return () => {
      window.removeEventListener("message", receive);
      document.removeEventListener("click", keepInDemo, true);
      document.removeEventListener("submit", stopExternalForm, true);
    };
  }, [router]);

  useEffect(() => {
    window.parent.postMessage(
      { type: "cubeship:demo:ready", path: pathname },
      window.location.origin,
    );
  }, [pathname]);

  return (
    <div className="public-demo-badge">
      <span>Interactive demo · Changes stay in this tab</span>
      <button type="button" onClick={() => window.location.assign("/demo")}>
        Reset demo
      </button>
    </div>
  );
}
