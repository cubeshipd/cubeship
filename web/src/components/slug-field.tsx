"use client";

import { TextField } from "@/components/text-field";
import { sanitize } from "@/lib/slug";

// Every slug field in the dashboard. The hint is fixed, and fixed here:
// it says the same thing at every level, and it does not grow with what
// is typed — a preview of the resulting reference wrapped onto three
// lines the moment a slug got long, which is the one moment you are
// least sure you have typed it right.
//
// It says nothing about a display name, and must not: there are none.
// A slug is the name, at every level — the hint used to promise that
// something friendlier came from it and could be changed later, which
// was an offer nothing has been able to honour since display names were
// removed.
//
// Why it is permanent is the part worth carrying: a slug is a component
// of the address other things are configured against — a registry path,
// a container name — so changing one would break whatever was pointed
// at the old one.
const HINT =
  "Lowercase letters, digits and dashes, and permanent: it becomes part of an address other things are configured against. A description carries whatever the name cannot.";

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
