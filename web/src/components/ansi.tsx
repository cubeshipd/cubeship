// Terminal colour, rendered rather than shown as the codes that produce
// it.
//
// A program writes its log for a terminal, and most of them colour it:
// the level, a timestamp dimmed, a field name in italics. Those arrive
// here as escape sequences, and a panel that prints them verbatim turns
// the one line somebody is reading into `[2m2026-…[0m [32m INFO[0m`.
//
// Stripping them was the other answer and it throws away something the
// author chose: in a log the colour of the level is half of how it is
// read. So the sequences are parsed and the styling is real, and what
// is not understood is dropped rather than printed — an unknown code is
// noise either way, and noise nobody can see is the better kind.
//
// Small on purpose. This reads SGR (`ESC [ … m`) and nothing else: a
// log is a stream of lines, not a screen to be addressed, so cursor
// movement and erasure are sequences that would mean nothing here even
// if they were honoured.

import type { CSSProperties, ReactNode } from "react";

// The eight, and their bright halves, as variables defined once in
// globals.css.
//
// They are the one set of colours here that does **not** follow the
// palette. An app writes red because something failed, and a red that
// came out pink under the pink theme would be the interface overruling
// what the program meant.
const COLORS: Record<number, string> = {
  30: "var(--ansi-black)",
  31: "var(--ansi-red)",
  32: "var(--ansi-green)",
  33: "var(--ansi-yellow)",
  34: "var(--ansi-blue)",
  35: "var(--ansi-magenta)",
  36: "var(--ansi-cyan)",
  37: "var(--ansi-white)",
  90: "var(--ansi-bright-black)",
  91: "var(--ansi-bright-red)",
  92: "var(--ansi-bright-green)",
  93: "var(--ansi-bright-yellow)",
  94: "var(--ansi-bright-blue)",
  95: "var(--ansi-bright-magenta)",
  96: "var(--ansi-bright-cyan)",
  97: "var(--ansi-bright-white)",
};

type Style = {
  color?: string;
  background?: string;
  bold?: boolean;
  dim?: boolean;
  italic?: boolean;
  underline?: boolean;
};

// sgr matches one escape sequence: `ESC [` , its parameters, and the
// letter that says what it is.
//
// Every sequence is consumed, and only `m` changes anything. A cursor
// move left in the text would be the code printed in the middle of a
// word — which is the bug this exists to fix, one sequence over.
// The escape byte is the point of this file, not an accident: the rule
// exists to catch one typed into a pattern by mistake.
// biome-ignore lint/suspicious/noControlCharactersInRegex: see above
const sgr = /\x1b\[([0-9;]*)([a-zA-Z])/g;

// apply folds one sequence's parameters into the running style.
//
// The parameters are read in order because that is what they mean:
// `1;31` is bold *and* red, and `0` in the middle of a list resets what
// came before it in the same sequence.
function apply(style: Style, params: string): Style {
  const codes = params === "" ? [0] : params.split(";").map((p) => Number(p) || 0);
  let next = { ...style };

  for (let i = 0; i < codes.length; i++) {
    const code = codes[i];
    switch (true) {
      case code === 0:
        next = {};
        break;
      case code === 1:
        next.bold = true;
        break;
      case code === 2:
        next.dim = true;
        break;
      case code === 3:
        next.italic = true;
        break;
      case code === 4:
        next.underline = true;
        break;
      case code === 22:
        next.bold = false;
        next.dim = false;
        break;
      case code === 23:
        next.italic = false;
        break;
      case code === 24:
        next.underline = false;
        break;
      case code === 39:
        next.color = undefined;
        break;
      case code === 49:
        next.background = undefined;
        break;
      case code in COLORS:
        next.color = COLORS[code];
        break;
      case code >= 40 && code <= 47:
        next.background = COLORS[code - 10];
        break;
      case code >= 100 && code <= 107:
        next.background = COLORS[code - 10];
        break;
      // 38 and 48 name a colour out of a 256-entry table or as three
      // bytes, and carry their arguments in the same parameter list. The
      // arguments are skipped whether or not the colour is understood,
      // or the numbers after them would be read as codes of their own —
      // `38;2;255;0;0` would turn a red into bold, and then into
      // nothing at all.
      case code === 38 || code === 48: {
        const where = code === 38 ? "color" : "background";
        if (codes[i + 1] === 5) {
          next[where] = indexed(codes[i + 2]);
          i += 2;
        } else if (codes[i + 1] === 2) {
          next[where] = `rgb(${codes[i + 2] ?? 0} ${codes[i + 3] ?? 0} ${codes[i + 4] ?? 0})`;
          i += 4;
        }
        break;
      }
    }
  }
  return next;
}

// indexed is xterm's 256-colour table: the first sixteen are the ones
// above, then a 6×6×6 cube, then twenty-four greys.
function indexed(n: number): string | undefined {
  if (n === undefined || n < 0 || n > 255) return undefined;
  if (n < 8) return COLORS[30 + n];
  if (n < 16) return COLORS[90 + (n - 8)];
  if (n < 232) {
    const c = n - 16;
    const step = (v: number) => (v === 0 ? 0 : 55 + v * 40);
    return `rgb(${step(Math.floor(c / 36))} ${step(Math.floor(c / 6) % 6)} ${step(c % 6)})`;
  }
  const grey = 8 + (n - 232) * 10;
  return `rgb(${grey} ${grey} ${grey})`;
}

function css(style: Style): CSSProperties | undefined {
  const out: CSSProperties = {};
  if (style.color) out.color = style.color;
  if (style.background) out.backgroundColor = style.background;
  if (style.bold) out.fontWeight = 700;
  if (style.italic) out.fontStyle = "italic";
  if (style.underline) out.textDecoration = "underline";
  // Dim is opacity rather than a darker colour, because it has to work
  // against whatever colour is already set — and against none, which is
  // the common case: a timestamp is usually dim and nothing else.
  if (style.dim) out.opacity = 0.65;
  return Object.keys(out).length > 0 ? out : undefined;
}

// Ansi renders text carrying terminal escapes.
//
// The style carries across newlines, because that is what a terminal
// does and what a program emitting a coloured block expects.
export function Ansi({ text }: { text: string }): ReactNode {
  // The common case by far, and worth not walking the string for: a log
  // with no escapes in it is one string and no elements at all.
  if (!text.includes("\x1b")) return text;

  const out: ReactNode[] = [];
  let style: Style = {};
  let at = 0;
  let key = 0;

  sgr.lastIndex = 0;
  for (let m = sgr.exec(text); m !== null; m = sgr.exec(text)) {
    if (m.index > at) {
      out.push(piece(text.slice(at, m.index), style, key++));
    }
    if (m[2] === "m") style = apply(style, m[1]);
    at = m.index + m[0].length;
  }
  if (at < text.length) out.push(piece(text.slice(at), style, key++));
  return out;
}

function piece(text: string, style: Style, key: number): ReactNode {
  const s = css(style);
  return s ? (
    <span key={key} style={s}>
      {text}
    </span>
  ) : (
    text
  );
}

// Strip is the same reading with nothing rendered, for the places a log
// is text rather than a panel: the filter that decides whether a line
// matches, and the file the download button writes.
//
// A filter that searched the raw bytes would answer "no line matches"
// for a word plainly on the screen, because what is between the cursor
// and the word is an escape nobody typed.
export function strip(text: string): string {
  return text.replace(sgr, "");
}
