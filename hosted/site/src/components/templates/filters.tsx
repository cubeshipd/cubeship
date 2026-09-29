"use client";

import { Search } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { Input } from "@/components/ui/field";

export function Filters({ q: initialQ }: { q?: string }) {
  const router = useRouter();
  const pathname = usePathname();
  const [q, setQ] = useState(initialQ ?? "");
  const sent = useRef(initialQ ?? "");
  useEffect(() => {
    if ((initialQ ?? "") !== sent.current) {
      sent.current = initialQ ?? "";
      setQ(initialQ ?? "");
    }
  }, [initialQ]);
  useEffect(() => {
    const next = q.trim();
    if (next === sent.current) return;
    const timer = setTimeout(() => {
      sent.current = next;
      router.replace(next ? `${pathname}?q=${encodeURIComponent(next)}` : pathname);
    }, 300);
    return () => clearTimeout(timer);
  }, [q, pathname, router]);
  return (
    <form className="template-search" onSubmit={(event) => event.preventDefault()}>
      <Search
        className="pointer-events-none absolute top-1/2 left-4 size-4 -translate-y-1/2 text-muted-foreground"
        aria-hidden
      />
      <Input
        type="search"
        value={q}
        onChange={(event) => setQ(event.target.value)}
        placeholder="Search templates"
        aria-label="Search templates"
        className="h-12 pl-11"
      />
    </form>
  );
}
