"use client";

import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import { ErrorAlert } from "@/components/error-alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api, type Releases } from "@/lib/api";
import { message } from "@/lib/errors";

const ReleaseNotesContext = createContext<() => void>(() => {});

export function useReleaseNotes() {
  return useContext(ReleaseNotesContext);
}

// What changed, shown once after an upgrade.
//
// The notes are in the daemon's own binary, so this is one request that
// almost always comes back with nothing to show — an instance behind a
// firewall answers it exactly as well as one that is not.
//
// Marked read when it is closed rather than when it is opened: closing
// is the act of having finished with it, and a dialog that marks itself
// read on the way up loses the notes to a stray refresh.
export function ReleaseNotes({ children }: { children?: ReactNode }) {
  const [state, setState] = useState<Releases | null>(null);
  const [view, setView] = useState<"unseen" | "history" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const dismissed = useRef(false);
  // Where focus lands when it opens. The dialog's default is the first
  // focusable element, which is a link somewhere down the notes, so it
  // opened scrolled to an old release instead of the newest.
  const notesRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    api
      .get<Releases>("/releases")
      .then((r) => {
        setState(r);
        setError(null);
        if (r.unseen.length > 0 && !dismissed.current) {
          setView((current) => current ?? "unseen");
        }
      })
      // A dashboard whose release notes could not be read is a
      // dashboard, and nothing about this is worth an error banner over
      // the screen somebody actually opened.
      .catch((err) => setError(message(err)));
  }, []);

  const openHistory = useCallback(() => {
    setView("history");
    if (!state) {
      setError(null);
      api
        .get<Releases>("/releases")
        .then(setState)
        .catch((err) => setError(message(err)));
    }
  }, [state]);

  const dismiss = useCallback(async () => {
    dismissed.current = true;
    setView(null);
    if (!state?.unseen.length) return;
    try {
      await api.post("/releases/seen", undefined);
      setState((s) => (s ? { ...s, unseen: [] } : s));
    } catch {
      // Nothing to do about it and nothing worth saying: the worst
      // case is the same notes once more.
    }
  }, [state]);

  const notes = (view === "history" ? state?.notes : state?.unseen) ?? [];
  const many = notes.length > 1;
  return (
    <ReleaseNotesContext.Provider value={openHistory}>
      {children}
      <Dialog open={view !== null} onOpenChange={(next) => !next && void dismiss()}>
        {/* A column rather than the primitive's grid: the header and the
          footer stay put and the notes scroll between them. Grid gave
          one tall box, so the button that dismisses this was below the
          fold of a long release. */}
        <DialogContent
          initialFocus={notesRef}
          className="flex max-h-[85vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl"
        >
          <DialogHeader className="shrink-0 border-border border-b p-4 pr-12">
            <DialogTitle>
              {view === "history"
                ? "Release notes"
                : many
                  ? `What changed in ${notes.length} releases`
                  : `Cubeship ${notes[0]?.version ?? ""}`}
            </DialogTitle>
            <DialogDescription>
              {view === "history"
                ? `Changes up to Cubeship ${state?.version ?? "the current version"}, newest first.`
                : many
                  ? "Changes since your last visit, newest first."
                  : (notes[0]?.summary ?? "Changes since your last visit.")}
            </DialogDescription>
          </DialogHeader>

          <div
            ref={notesRef}
            tabIndex={-1}
            className="min-w-0 flex-1 space-y-8 overflow-y-auto p-4 outline-none"
          >
            {error && <ErrorAlert error={error} />}
            {!state && !error && (
              <p className="text-sm text-muted-foreground">Loading release notes…</p>
            )}
            {state && notes.length === 0 && (
              <p className="text-sm text-muted-foreground">No release notes for this build yet.</p>
            )}
            {notes.map((note) => (
              <section key={note.version} className="space-y-3">
                {(many || view === "history") && (
                  <header className="border-border border-b pb-2">
                    <h2 className="font-medium text-foreground text-sm">Cubeship {note.version}</h2>
                    <p className="mt-1 text-muted-foreground text-xs">
                      {note.date} · {note.summary}
                    </p>
                  </header>
                )}
                <Notes body={note.body} />
              </section>
            ))}
          </div>

          <DialogFooter className="mx-0 mb-0 shrink-0 rounded-none border-border border-t p-4">
            <Button onClick={() => void dismiss()}>
              {view === "history" ? "Close" : "Got it"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </ReleaseNotesContext.Provider>
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
