"use client";

import { TextField } from "@/components/text-field";
import { sanitize } from "@/lib/slug";

// Every slug field in the dashboard. The hint is fixed, and fixed here:
// it says the same thing at every level, and it does not grow with what
// is typed — a preview of the resulting reference wrapped onto three
// lines the moment a slug got long, which is the one moment you are
// least sure you have typed it right.
const HINT =
  "Lowercase letters, digits and dashes. Permanent — a display name comes from it and can be changed later, this cannot.";

export function SlugField({
  label = "Slug",
  value,
  onChange,
  ...props
}: Omit<React.ComponentProps<typeof TextField>, "label" | "hint" | "onChange"> & {
  label?: string;
  // Already sanitized: a slug field refuses what the daemon would, as
  // it is typed, rather than explaining it after a failed submit.
  onChange: (value: string) => void;
}) {
  return (
    <TextField
      label={label}
      hint={HINT}
      spellCheck={false}
      value={value}
      onChange={(e) => onChange(sanitize(e.target.value))}
      {...props}
    />
  );
}
