"use client";

import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { cn } from "cn";
import { RefreshCwIcon, TerminalIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";

// A shell, in the page.
//
// **The daemon speaks two kinds of frame, and this reads them apart.** A
// binary frame is terminal bytes — written to xterm untouched, escapes and
// all, because a full-screen program is addressing a screen and xterm is
// one. A text frame is a sentence about the session: it started, the
// window changed, the program ended, something refused. See
// internal/platform/terminal on the other end.
//
// This module is only ever loaded through `next/dynamic` (see
// `TerminalPanel`), so xterm and its stylesheet are in a chunk of their
// own: the app page that nobody opens a shell from does not carry them.

type State =
  | { kind: "password" }
  | { kind: "connecting" }
  | { kind: "open" }
  | { kind: "closed"; reason: string; failed: boolean };

// Read off the CSS variables the log panel already uses, so a program's
// red is the same red on both — see "A log is coloured" in dashboard.md.
function theme() {
  const css = getComputedStyle(document.documentElement);
  const v = (name: string) => css.getPropertyValue(name).trim() || undefined;
  return {
    background: "#000000",
    foreground: v("--ansi-white"),
    cursor: v("--ansi-green"),
    selectionBackground: "#2de2e655",
    black: v("--ansi-black"),
    red: v("--ansi-red"),
    green: v("--ansi-green"),
    yellow: v("--ansi-yellow"),
    blue: v("--ansi-blue"),
    magenta: v("--ansi-magenta"),
    cyan: v("--ansi-cyan"),
    white: v("--ansi-white"),
    brightBlack: v("--ansi-bright-black"),
    brightRed: v("--ansi-bright-red"),
    brightGreen: v("--ansi-bright-green"),
    brightYellow: v("--ansi-bright-yellow"),
    brightBlue: v("--ansi-bright-blue"),
    brightMagenta: v("--ansi-bright-magenta"),
    brightCyan: v("--ansi-bright-cyan"),
    brightWhite: v("--ansi-bright-white"),
  };
}

export default function TerminalSession({
  path,
  query = {},
  password = false,
  className,
}: {
  // The session's address under /api: `/apps/<ref>/shell`, or
  // `/nodes/<name>/shell`.
  path: string;
  query?: Record<string, string>;
  // A root shell asks for the password before it opens. The daemon asks
  // too — this is where the person is asked.
  password?: boolean;
  // The terminal's height. Given by the caller and held from the first
  // render, so nothing on the page moves when the shell connects.
  className?: string;
}) {
  const host = useRef<HTMLDivElement>(null);
  const socket = useRef<WebSocket | null>(null);
  const term = useRef<Terminal | null>(null);
  const fit = useRef<FitAddon | null>(null);
  const [state, setState] = useState<State>(
    password ? { kind: "password" } : { kind: "connecting" },
  );
  const [target, setTarget] = useState<string | null>(null);
  const [secret, setSecret] = useState("");
  // Read when the socket opens. A ref, because typing the password is not
  // a reason to reconnect.
  const secretRef = useRef("");
  secretRef.current = secret;
  // The query as one string, so a caller passing a fresh object each
  // render does not reopen the session each render.
  const search = new URLSearchParams(query).toString();
  // Bumped to open a fresh session; the terminal itself is kept, so the
  // last session's output is still there to read above the new prompt.
  const [attempt, setAttempt] = useState(password ? 0 : 1);

  // The terminal, once for the life of the panel.
  useEffect(() => {
    if (!host.current) return;
    // The family next/font registered, not `--font-mono`: that one is a
    // `var()` reference, which is CSS's to resolve and not a canvas's.
    const mono = getComputedStyle(document.documentElement)
      .getPropertyValue("--font-jbmono")
      .trim();
    const t = new Terminal({
      cursorBlink: true,
      fontFamily: `${mono ? `${mono}, ` : ""}ui-monospace, SFMono-Regular, monospace`,
      fontSize: 13,
      lineHeight: 1.2,
      scrollback: 5000,
      theme: theme(),
      allowProposedApi: false,
    });
    const f = new FitAddon();
    t.loadAddon(f);
    t.open(host.current);
    f.fit();
    // A cell is measured in the font it is drawn in. Measured before the
    // font has loaded, every column is the fallback's width and the
    // prompt lands in the wrong place.
    document.fonts.ready.then(() => f.fit());
    term.current = t;
    fit.current = f;

    const encoder = new TextEncoder();
    const send = (data: Uint8Array) => {
      if (socket.current?.readyState === WebSocket.OPEN) socket.current.send(data);
    };
    const typed = t.onData((d) => send(encoder.encode(d)));
    const binary = t.onBinary((d) => send(Uint8Array.from(d, (c) => c.charCodeAt(0))));
    const resized = t.onResize(({ cols, rows }) => {
      if (socket.current?.readyState === WebSocket.OPEN) {
        socket.current.send(JSON.stringify({ type: "resize", cols, rows }));
      }
    });

    // The box is sized by the page; the terminal follows it.
    const observer = new ResizeObserver(() => f.fit());
    observer.observe(host.current);

    return () => {
      observer.disconnect();
      typed.dispose();
      binary.dispose();
      resized.dispose();
      socket.current?.close();
      t.dispose();
      term.current = null;
    };
  }, []);

  // A session, each time `attempt` moves.
  useEffect(() => {
    const t = term.current;
    if (attempt === 0 || !t) return;
    setState({ kind: "connecting" });
    setTarget(null);

    const params = new URLSearchParams(search);
    params.set("cols", String(t.cols));
    params.set("rows", String(t.rows));
    const scheme = window.location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(`${scheme}://${window.location.host}/api${path}?${params}`);
    ws.binaryType = "arraybuffer";
    socket.current = ws;
    let ended = false;
    const end = (reason: string, failed: boolean) => {
      if (ended) return;
      ended = true;
      setState({ kind: "closed", reason, failed });
    };

    ws.onopen = () => {
      if (password) ws.send(JSON.stringify({ type: "auth", password: secretRef.current }));
    };
    ws.onmessage = (event) => {
      if (typeof event.data !== "string") {
        t.write(new Uint8Array(event.data as ArrayBuffer));
        return;
      }
      let m: { type: string; code?: number; message?: string; target?: string };
      try {
        m = JSON.parse(event.data);
      } catch {
        return;
      }
      if (m.type === "ready") {
        setTarget(m.target ?? null);
        setState({ kind: "open" });
        fit.current?.fit();
        t.focus();
      } else if (m.type === "exit") {
        end(`The shell exited${m.code ? ` with status ${m.code}` : ""}.`, false);
      } else if (m.type === "error") {
        end(m.message ?? "The session ended.", true);
      }
    };
    ws.onclose = () =>
      end("The connection closed. The instance may have restarted, or the network dropped.", true);
    ws.onerror = () => end("Could not connect to the instance.", true);

    return () => {
      ended = true;
      ws.close();
    };
  }, [attempt, path, password, search]);

  const reconnect = useCallback(() => {
    if (password) {
      setSecret("");
      setState({ kind: "password" });
      return;
    }
    setAttempt((n) => n + 1);
  }, [password]);

  return (
    <div className="flex flex-col gap-2">
      {/* One line, always the same height, so its contents changing
          never moves the terminal under it. */}
      <div className="flex h-8 items-center justify-between gap-3">
        <span className="flex min-w-0 items-center gap-2 font-mono text-xs text-muted-foreground">
          <TerminalIcon className="size-3.5 shrink-0" />
          <span className="truncate">
            {state.kind === "open" && target
              ? target
              : state.kind === "connecting"
                ? "Connecting…"
                : state.kind === "password"
                  ? "Confirm your password to open a root shell"
                  : state.kind === "closed"
                    ? state.reason
                    : ""}
          </span>
        </span>
        {state.kind === "closed" && (
          <Button variant="outline" size="sm" onClick={reconnect}>
            <RefreshCwIcon />
            Reconnect
          </Button>
        )}
      </div>
      <div className={cn("relative border border-border bg-black", className)}>
        <div ref={host} className="absolute inset-0 p-2" />
        {state.kind === "password" && (
          <form
            className="absolute inset-0 flex items-center justify-center bg-black/85 p-4"
            onSubmit={(e) => {
              e.preventDefault();
              if (secret) setAttempt((n) => n + 1);
            }}
          >
            <div className="w-full max-w-sm space-y-3">
              <TextField
                label="Password"
                type="password"
                autoComplete="current-password"
                autoFocus
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
                hint="This is a root shell on the machine. Everything it does is done as root, and the session is written to the audit log."
              />
              <ActionButton type="submit" disabled={!secret}>
                Open shell
              </ActionButton>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}
