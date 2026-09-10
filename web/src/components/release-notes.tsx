"use client";

import { useCallback, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api, type Release, type Releases } from "@/lib/api";

// What changed, shown once after an upgrade.
//
// The notes are in the daemon's own binary, so this is one request that
// almost always comes back with nothing to show — an instance behind a
// firewall answers it exactly as well as one that is not.
//
// Marked read when it is closed rather than when it is opened: closing
// is the act of having finished with it, and a dialog that marks itself
// read on the way up loses the notes to a stray refresh.
export function ReleaseNotes() {
  const [pending, setPending] = useState<Release[] | null>(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    api
      .get<Releases>("/releases")
      .then((r) => {
        if (r.unseen.length > 0) {
          setPending(r.unseen);
          setOpen(true);
        }
      })
      // A dashboard whose release notes could not be read is a
      // dashboard, and nothing about this is worth an error banner over
      // the screen somebody actually opened.
      .catch(() => {});
  }, []);

  // **Marking it read is not the dialog closing.** `onOpenChange` fires
  // for the dialog's own triggers — the X, Escape, a click outside —
  // and not for a button that sets `open` itself. Wiring the POST there
  // alone meant "Got it" closed the dialog and told the daemon nothing,
  // so the next reload showed the same notes again.
  //
  // Awaited before closing, because a reload a moment later would
  // otherwise cancel a request that is still in flight — which is
  // exactly the sequence somebody who just read the notes performs.
  const dismiss = useCallback(async () => {
    setOpen(false);
    try {
      await api.post("/releases/seen", undefined);
    } catch {
      // Nothing to do about it and nothing worth saying: the worst
      // case is the same notes once more.
    }
  }, []);

  if (!pending) return null;

  const many = pending.length > 1;
  return (
    <Dialog open={open} onOpenChange={(next) => !next && dismiss()}>
      {/* A column rather than the primitive's grid: the header and the
          footer stay put and the notes scroll between them. Grid gave
          one tall box, so the button that dismisses this was below the
          fold of a long release. */}
      <DialogContent className="flex max-h-[85vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
        <DialogHeader className="shrink-0 border-border border-b p-4 pr-12">
          <DialogTitle>
            {many ? `What changed in ${pending.length} releases` : `Cubeship ${pending[0].version}`}
          </DialogTitle>
          <DialogDescription>
            {many
              ? "This instance has been upgraded past more than one release. Newest first."
              : pending[0].summary}
          </DialogDescription>
        </DialogHeader>

        <div className="min-w-0 flex-1 space-y-8 overflow-y-auto p-4">
          {pending.map((note) => (
            <section key={note.version} className="space-y-3">
              {many && (
                <header className="flex items-baseline gap-3 border-border border-b pb-1">
                  <h2 className="font-mono text-primary text-sm">{note.version}</h2>
                  <span className="font-mono text-[11px] text-muted-foreground">{note.date}</span>
                </header>
              )}
              <Notes body={note.body} />
            </section>
          ))}
        </div>

        <DialogFooter className="shrink-0 border-border border-t p-4">
          <Button onClick={dismiss}>Got it</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// Notes renders the subset of Markdown a release note is written in:
// `##` headings, paragraphs, `- ` lists, fenced code blocks, and inline
// `**bold**`, `` `code` `` and links.
//
// **Hand-written rather than a Markdown library**, and the reason is
// what the input is: these files ship inside the daemon's own binary and
// are written by whoever cuts the release. There is no user content
// here, no HTML to sanitize and no long tail of syntax to support — a
// dependency would be carrying a parser for a document format nobody
// else can supply.
//
// Anything it does not recognise comes out as a paragraph, which is the
// failure worth having: a heading that renders as text still reads.
export function Notes({ body }: { body: string }) {
  const blocks: React.ReactNode[] = [];
  const lines = body.split("\n");
  let list: string[] = [];
  let para: string[] = [];
  let code: string[] | null = null;

  const flushList = () => {
    if (list.length === 0) return;
    blocks.push(
      <ul
        key={`l${blocks.length}`}
        className="ml-4 list-disc space-y-1.5 break-words text-xs leading-relaxed"
      >
        {list.map((item) => (
          <li key={item}>{inline(item)}</li>
        ))}
      </ul>,
    );
    list = [];
  };
  const flushPara = () => {
    if (para.length === 0) return;
    blocks.push(
      <p
        key={`p${blocks.length}`}
        className="break-words text-muted-foreground text-xs leading-relaxed"
      >
        {inline(para.join(" "))}
      </p>,
    );
    para = [];
  };
  const flushCode = () => {
    if (code === null) return;
    blocks.push(
      // **The scroll is the block's, not the dialog's.** A release note
      // is full of commands, and one long enough to overflow made the
      // whole dialog scroll sideways — so the reader lost the prose
      // too, and the button at the bottom moved out from under the
      // pointer.
      <pre
        key={`c${blocks.length}`}
        className="overflow-x-auto border border-border bg-background p-3 font-mono text-[11px] text-foreground"
      >
        {code.join("\n")}
      </pre>,
    );
    code = null;
  };

  for (const raw of lines) {
    const line = raw.trimEnd();
    // A fence opens and closes a block, and everything between it is
    // taken as written: a command with a URL in it is not prose, and
    // wrapping one is how somebody copies half of it.
    if (line.trimStart().startsWith("```")) {
      if (code === null) {
        flushList();
        flushPara();
        code = [];
      } else {
        flushCode();
      }
      continue;
    }
    if (code !== null) {
      code.push(raw);
      continue;
    }
    if (line.trim() === "") {
      flushList();
      flushPara();
    } else if (line.startsWith("## ")) {
      flushList();
      flushPara();
      blocks.push(
        <h3
          key={`h${blocks.length}`}
          className="pt-2 font-medium text-foreground text-sm uppercase tracking-wide"
        >
          {line.slice(3)}
        </h3>,
      );
    } else if (line.startsWith("- ")) {
      flushPara();
      list.push(line.slice(2));
    } else if (list.length > 0 && line.startsWith("  ")) {
      // A wrapped list item, which is what a note written to 72
      // columns is full of.
      list[list.length - 1] += ` ${line.trim()}`;
    } else {
      flushList();
      para.push(line.trim());
    }
  }
  flushList();
  flushPara();
  // A note that ends inside a fence is one somebody mistyped. Rendering
  // what is there beats dropping the rest of the release.
  flushCode();

  return <div className="min-w-0 space-y-3">{blocks}</div>;
}

// inline renders `**bold**`, `` `code` `` and [links](url) in one pass.
const INLINE = /(\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\([^)]+\))/g;

function inline(text: string): React.ReactNode[] {
  return text.split(INLINE).map((part, i) => {
    // The index is the key on purpose: the parts are a static split of
    // one line of text that never reorders.
    const key = `${i}`;
    if (part.startsWith("**") && part.endsWith("**")) {
      return (
        <strong key={key} className="font-medium text-foreground">
          {part.slice(2, -2)}
        </strong>
      );
    }
    if (part.startsWith("`") && part.endsWith("`")) {
      return (
        <code key={key} className="font-mono text-[11px] text-foreground">
          {part.slice(1, -1)}
        </code>
      );
    }
    const link = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(part);
    if (link) {
      return (
        <a
          key={key}
          href={link[2]}
          target="_blank"
          rel="noreferrer"
          className="text-primary underline-offset-2 hover:underline"
        >
          {link[1]}
        </a>
      );
    }
    return part;
  });
}
