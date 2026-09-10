"use client";

import { useEffect, useState } from "react";
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

  if (!pending) return null;

  const many = pending.length > 1;
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) {
          api.post("/releases/seen", undefined).catch(() => {});
        }
      }}
    >
      <DialogContent className="max-h-[80vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {many ? `What changed in ${pending.length} releases` : `Cubeship ${pending[0].version}`}
          </DialogTitle>
          <DialogDescription>
            {many
              ? "This instance has been upgraded past more than one release. Newest first."
              : pending[0].summary}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-8">
          {pending.map((note) => (
            <section key={note.version} className="space-y-3">
              {many && (
                <header className="flex items-baseline gap-3 border-b border-border pb-1">
                  <h2 className="font-mono text-sm text-primary">{note.version}</h2>
                  <span className="font-mono text-[11px] text-muted-foreground">{note.date}</span>
                </header>
              )}
              <Notes body={note.body} />
            </section>
          ))}
        </div>

        <DialogFooter>
          <Button onClick={() => setOpen(false)}>Got it</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// Notes renders the subset of Markdown a release note is written in:
// `##` headings, paragraphs, `- ` lists, and inline `**bold**`,
// `` `code` `` and links.
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
function Notes({ body }: { body: string }) {
  const blocks: React.ReactNode[] = [];
  const lines = body.split("\n");
  let list: string[] = [];
  let para: string[] = [];

  const flushList = () => {
    if (list.length === 0) return;
    blocks.push(
      <ul key={`l${blocks.length}`} className="ml-4 list-disc space-y-1.5 text-xs leading-relaxed">
        {list.map((item, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: the list is static text from one release note
          <li key={i}>{inline(item)}</li>
        ))}
      </ul>,
    );
    list = [];
  };
  const flushPara = () => {
    if (para.length === 0) return;
    blocks.push(
      <p key={`p${blocks.length}`} className="text-xs leading-relaxed text-muted-foreground">
        {inline(para.join(" "))}
      </p>,
    );
    para = [];
  };

  for (const raw of lines) {
    const line = raw.trimEnd();
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

  return <div className="space-y-3">{blocks}</div>;
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
