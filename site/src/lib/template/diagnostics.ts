export type Pos = { line: number; column: number; offset: number };
export type Range = { start: Pos; end: Pos };
export type Severity = "error" | "warning" | "info";

export type Diagnostic = {
  severity: Severity;
  // Stable and linkable: the docs have a section per code.
  code: string;
  message: string;
  path: (string | number)[];
  range?: Range;
  hint?: string;
};

// Only an error stops a publish. A warning is advice the author may keep.
export function blocks(diagnostics: Diagnostic[]): boolean {
  return diagnostics.some((d) => d.severity === "error");
}

export type Locate = (path: (string | number)[], target?: "value" | "key") => Range | undefined;
