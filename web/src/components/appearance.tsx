"use client";

import { CheckIcon } from "lucide-react";
import { SectionHeader } from "@/components/section-header";
import { useSession } from "@/components/session-context";
import { useTheme } from "@/components/theme";
import { Card, CardContent } from "@/components/ui/card";

// What each palette is called on screen, and the two colours that say
// what it is without having to be applied.
//
// The swatch is the accent over the palette's own background, because
// the accent alone tells you nothing: half of what makes one of these
// different is what the surfaces underneath do.
const PALETTES: Record<string, { label: string; note: string; bg: string; fg: string }> = {
  "": { label: "Cubeship", note: "Cyan on near-black", bg: "#05070a", fg: "#2de2e6" },
  mono: { label: "Mono", note: "No hue at all", bg: "#000000", fg: "#ededed" },
  hacker: { label: "Hacker", note: "Green on black", bg: "#030603", fg: "#3dff7a" },
  red: { label: "Red", note: "", bg: "#0a0405", fg: "#ff4d5e" },
  orange: { label: "Orange", note: "", bg: "#0a0603", fg: "#ff9d2e" },
  pink: { label: "Pink", note: "", bg: "#0a0409", fg: "#ff5cb8" },
  purple: { label: "Purple", note: "", bg: "#07050e", fg: "#a97bff" },
  blue: { label: "Blue", note: "Blue on navy", bg: "#03060f", fg: "#4d8dff" },
};

// Appearance is the palette this person sees the dashboard in.
//
// **The swatches are painted from literal colours, not from the CSS
// variables**, and they have to be: every one of these is drawn while a
// different palette is live, and a variable would make every one of them the
// same colour as the current one.
export function Appearance() {
  const me = useSession();
  const { theme, choose } = useTheme(me);
  // The daemon says which it has, so a palette added there appears here
  // without this list being edited. One it does not know about is
  // dropped rather than shown as a blank.
  const offered = ["", ...(me.themes ?? [])].filter((t) => t === "" || t in PALETTES);

  return (
    <>
      <SectionHeader
        title="Theme"
        sub="Colour only, and every one of them dark. This is a console read beside a terminal — the layout, the type and the square corners are the same whichever you pick, and they are what the product is."
      />
      <Card>
        <CardContent>
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {offered.map((name) => {
              const palette = PALETTES[name];
              const active = (theme || "") === name;
              return (
                <button
                  key={name || "default"}
                  type="button"
                  onClick={() => choose(name)}
                  aria-pressed={active}
                  className={
                    active
                      ? "flex items-center gap-3 border border-primary bg-secondary p-3 text-left"
                      : "flex items-center gap-3 border border-border p-3 text-left transition-colors hover:border-border-strong hover:bg-secondary"
                  }
                >
                  <span
                    className="flex size-9 shrink-0 items-center justify-center border"
                    style={{ background: palette.bg, borderColor: palette.fg }}
                  >
                    <span className="size-3.5" style={{ background: palette.fg }} />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium text-xs uppercase tracking-wide">
                      {palette.label}
                    </span>
                    {palette.note && (
                      <span className="block truncate text-[11px] text-muted-foreground">
                        {palette.note}
                      </span>
                    )}
                  </span>
                  {active && <CheckIcon className="size-4 shrink-0 text-primary" />}
                </button>
              );
            })}
          </div>
          <p className="pt-4 text-[11px] text-muted-foreground">
            Saved to your account, so it follows you to another browser rather than staying on this
            machine.
          </p>
        </CardContent>
      </Card>
    </>
  );
}
