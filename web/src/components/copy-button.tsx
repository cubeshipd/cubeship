"use client";

import { cn } from "cn";
import { CheckIcon, CopyIcon } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";

// copyText puts a value on the clipboard and answers whether it got
// there.
//
// **navigator.clipboard exists only in a secure context**, and this
// dashboard is reached at http://<ip>:3000 until an instance has a
// domain. The textarea is not a nicety, it is the path most people are
// on before they have DNS — and it is here, once, because everything
// that copies needs it and only one of them can be the place it is
// explained.
export async function copyText(value: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(value);
      return true;
    }
    const field = document.createElement("textarea");
    field.value = value;
    field.setAttribute("readonly", "");
    field.style.position = "fixed";
    field.style.opacity = "0";
    document.body.appendChild(field);
    field.select();
    document.execCommand("copy");
    document.body.removeChild(field);
    return true;
  } catch {
    return false;
  }
}

// Copy one value, and say so where it was clicked rather than in a
// toast: the value is the answer, and the button is where the eye
// already is.
export function CopyButton({
  value,
  label = "Copy",
  className,
}: {
  value: string;
  label?: string;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    // A failed copy is not worth an error dialog: the value is on
    // screen to select by hand.
    if (await copyText(value)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }
  }

  return (
    <Button
      type="button"
      variant="ghost"
      size="xs"
      aria-label={label}
      title={value}
      onClick={(e) => {
        e.stopPropagation();
        copy();
      }}
      className={cn("text-muted-foreground", className)}
    >
      {copied ? <CheckIcon className="size-3.5 text-primary" /> : <CopyIcon className="size-3.5" />}
    </Button>
  );
}
