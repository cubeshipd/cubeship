"use client";

import { yaml } from "@codemirror/lang-yaml";
import { type Diagnostic as CmDiagnostic, linter, lintGutter } from "@codemirror/lint";
import { EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { basicSetup } from "codemirror";
import { type Ref, useEffect, useImperativeHandle, useRef } from "react";
import type { Diagnostic } from "@/lib/template";

export type TemplateEditorHandle = {
  jumpTo(range: { start: { offset: number }; end: { offset: number } }): void;
};

// The same module the server publishes with, so what the author sees is
// what the publish will say.
export function TemplateEditor({
  value,
  onChange,
  diagnose,
  editorRef,
}: {
  value: string;
  onChange: (next: string) => void;
  diagnose: (source: string) => Diagnostic[];
  editorRef?: Ref<TemplateEditorHandle>;
}) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  // CodeMirror owns the document once mounted; the callbacks stay fresh
  // through refs instead of tearing the editor down on every keystroke,
  // which is what re-running the mount effect on `value`/`onChange` would do.
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const diagnoseRef = useRef(diagnose);
  diagnoseRef.current = diagnose;

  useImperativeHandle(
    editorRef,
    () => ({
      jumpTo(range) {
        const editor = view.current;
        if (!editor) return;
        editor.dispatch({
          selection: { anchor: range.start.offset, head: range.end.offset },
          effects: EditorView.scrollIntoView(range.start.offset, { y: "center" }),
        });
        editor.focus();
      },
    }),
    [],
  );

  // Mounted once with `value` as the initial document only: after that
  // the callbacks above are read from refs, never from this closure.
  // biome-ignore lint/correctness/useExhaustiveDependencies: mount-only, see above.
  useEffect(() => {
    if (!host.current) return;

    const editor = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: value,
        extensions: [
          basicSetup,
          yaml(),
          lintGutter(),
          linter(
            (state): CmDiagnostic[] =>
              diagnoseRef.current(state.state.doc.toString()).map((found) => ({
                from: found.range?.start.offset ?? 0,
                to: found.range?.end.offset ?? Math.min(1, state.state.doc.length),
                severity: found.severity === "info" ? "info" : found.severity,
                message: found.hint ? `${found.message} — ${found.hint}` : found.message,
                source: found.code,
              })),
            { delay: 300 },
          ),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) onChangeRef.current(update.state.doc.toString());
          }),
          EditorView.theme({
            "&": { fontSize: "13px", backgroundColor: "transparent" },
            ".cm-gutters": {
              backgroundColor: "transparent",
              borderRight: "1px solid var(--color-fd-border)",
            },
          }),
        ],
      }),
    });
    view.current = editor;

    return () => {
      editor.destroy();
      view.current = null;
    };
  }, []);

  return <div ref={host} className="hud-frame min-h-[28rem] border border-fd-border" />;
}
