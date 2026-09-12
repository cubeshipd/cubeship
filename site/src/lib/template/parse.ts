import { isMap, isSeq, LineCounter, type Node, type Pair, parseDocument } from "yaml";
import type { Diagnostic, Locate, Pos, Range } from "./diagnostics";

type YamlError = { pos: [number, number]; message: string };

function position(counter: LineCounter, offset: number): Pos {
  const { line, col } = counter.linePos(offset);
  return { line, column: col, offset };
}

// yaml reports one syntax break as several cascading messages sharing an
// overlapping span — the first, least specific one often starts further
// back than where the mistake is visible. Keep only the last message in
// each overlapping run, and anchor on its end: where parsing gave up.
function mergeErrorSpans(errors: readonly YamlError[]): YamlError[] {
  const merged: YamlError[] = [];
  for (const error of errors) {
    const last = merged[merged.length - 1];
    if (last && error.pos[0] <= last.pos[1]) {
      merged[merged.length - 1] = error;
    } else {
      merged.push(error);
    }
  }
  return merged;
}

// yaml embeds its own "at line X, column Y" position and a caret snippet
// in the message. range is the single source of position now, so a
// message and a range that each name a different line would disagree.
function stripPosition(message: string): string {
  return message.replace(/\s*at line \d+, column \d+:[\s\S]*$/, "");
}

function rangeOf(
  counter: LineCounter,
  node: { range?: [number, number, number] | null },
): Range | undefined {
  if (!node.range) return undefined;
  const [start, end] = node.range;
  return { start: position(counter, start), end: position(counter, end) };
}

// The key token rather than the value, for the errors that are about a
// field existing at all.
function keyRange(counter: LineCounter, parent: unknown, key: string | number): Range | undefined {
  if (!isMap(parent)) return undefined;
  const pair = parent.items.find((item: Pair) => (item.key as { value?: unknown })?.value === key);
  return pair?.key ? rangeOf(counter, pair.key as Node) : undefined;
}

export function parseSource(source: string): {
  value?: unknown;
  locate: Locate;
  diagnostics: Diagnostic[];
} {
  const counter = new LineCounter();
  const doc = parseDocument(source, { lineCounter: counter, keepSourceTokens: true });

  const locate: Locate = (path, target = "value") => {
    // Walk down as far as the document goes, so a missing key still
    // points somewhere useful: its parent.
    let deepest: Range | undefined = doc.contents
      ? rangeOf(counter, doc.contents as Node)
      : undefined;
    let parent: unknown = doc.contents;

    for (let i = 0; i < path.length; i++) {
      const node = doc.getIn(path.slice(0, i + 1), true);
      if (!node) break;
      if (i === path.length - 1 && target === "key") {
        const key = keyRange(counter, parent, path[i]);
        if (key) return key;
      }
      deepest = rangeOf(counter, node as Node) ?? deepest;
      parent = node;
    }

    return deepest;
  };

  const diagnostics: Diagnostic[] = mergeErrorSpans(doc.errors).map((error) => {
    const point = position(counter, error.pos[1]);
    return {
      severity: "error" as const,
      code: "yaml.syntax",
      message: stripPosition(error.message),
      path: [],
      range: { start: point, end: point },
    };
  });

  if (diagnostics.length > 0) return { locate, diagnostics };

  if (doc.contents === null || (!isMap(doc.contents) && !isSeq(doc.contents))) {
    return {
      locate,
      diagnostics: [
        {
          severity: "error",
          code: "yaml.empty",
          message:
            "the file is empty: a template is a mapping with at least version, project and apps",
          path: [],
        },
      ],
    };
  }

  return { value: doc.toJS(), locate, diagnostics: [] };
}
