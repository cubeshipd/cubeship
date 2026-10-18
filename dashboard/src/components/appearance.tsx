"use client";

import { CheckIcon, MoonIcon, SunIcon } from "lucide-react";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { useTheme } from "@/components/theme";
import { Card, CardContent } from "@/components/ui/card";

// Preview colours are literal so each choice remains visible before it is applied.
const ACCENTS = {
  cyan: { label: "Cyan", dark: "#2de2e6", light: "#007c85" },
  neutral: { label: "Neutral", dark: "#ededed", light: "#0a0a0a" },
  green: { label: "Green", dark: "#3dff7a", light: "#16793a" },
  red: { label: "Red", dark: "#ff4d5e", light: "#c72c42" },
  orange: { label: "Orange", dark: "#ff9d2e", light: "#a64b00" },
  pink: { label: "Pink", dark: "#ff5cb8", light: "#af246f" },
  purple: { label: "Purple", dark: "#a97bff", light: "#7041bb" },
  blue: { label: "Blue", dark: "#4d8dff", light: "#265db8" },
} as const;

export function Appearance() {
  const me = useSession();
  const { theme, choose, busy } = useTheme(me);
  const base = theme.startsWith("light-") ? "light" : "dark";
  const accent = theme.slice(base.length + 1);
  const offered = new Set(me.themes ?? []);

  return (
    <Card>
      <CardContent className="space-y-5 [&>.dashboard-section-header]:mb-0">
        <SectionHeader title="Appearance" sub="Choose a base and an accent for your dashboard." />
        <fieldset className="space-y-2">
          <legend className="text-xs font-medium text-muted-foreground">Base</legend>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            {(["dark", "light"] as const).map((nextBase) => (
              <button
                key={nextBase}
                type="button"
                disabled={busy || !offered.has(`${nextBase}-${accent}`)}
                onClick={() => choose(`${nextBase}-${accent}`)}
                aria-pressed={base === nextBase}
                className={`flex h-12 items-center gap-3 rounded-lg border px-3 text-sm transition-colors hover:border-border-strong focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50 disabled:opacity-50 ${base === nextBase ? "border-primary bg-primary/5" : "border-border bg-background"}`}
              >
                {nextBase === "dark" ? (
                  <MoonIcon aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
                ) : (
                  <SunIcon aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
                )}
                <span className="flex-1 text-left">{nextBase === "dark" ? "Dark" : "Light"}</span>
                {base === nextBase && <CheckIcon className="size-4 shrink-0 text-primary" />}
              </button>
            ))}
          </div>
        </fieldset>
        <fieldset className="space-y-2">
          <legend className="text-xs font-medium text-muted-foreground">Accent</legend>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            {Object.entries(ACCENTS).map(([name, palette]) => {
              const active = accent === name;
              const preview =
                name === "neutral"
                  ? base === "dark"
                    ? "linear-gradient(135deg, #000 50%, #fff 50%)"
                    : "linear-gradient(135deg, #fff 50%, #000 50%)"
                  : palette[base];
              return (
                <button
                  key={name}
                  type="button"
                  disabled={busy || !offered.has(`${base}-${name}`)}
                  onClick={() => choose(`${base}-${name}`)}
                  aria-pressed={active}
                  className={`flex h-12 min-w-0 items-center gap-3 rounded-lg border px-3 text-sm transition-colors hover:border-border-strong focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50 disabled:opacity-50 ${active ? "border-primary bg-primary/5" : "border-border bg-background"}`}
                >
                  <span
                    aria-hidden="true"
                    className="size-5 shrink-0 rounded-full ring-1 ring-foreground/25"
                    style={{ background: preview }}
                  />
                  <span className="min-w-0 flex-1 truncate text-left">{palette.label}</span>
                  {active && <CheckIcon className="size-4 shrink-0 text-primary" />}
                </button>
              );
            })}
          </div>
        </fieldset>
      </CardContent>
    </Card>
  );
}
