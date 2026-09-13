import { advise } from "./advice";
import { blocks, type Diagnostic } from "./diagnostics";
import { type NormalizedManifest, normalize } from "./normalize";
import { parseSource } from "./parse";
import { fromZod, manifestSchema } from "./schema";
import { checkSemantics } from "./semantics";

export type ValidationResult = {
  ok: boolean;
  diagnostics: Diagnostic[];
  manifest?: NormalizedManifest;
};

export type { Diagnostic } from "./diagnostics";
export type { NormalizedApp, NormalizedManifest } from "./normalize";
export { SCHEMA_VERSION } from "./normalize";

// Each layer runs only when the one before it left something to work
// with: semantics over a half-parsed file would report nonsense.
export function validateTemplate(source: string): ValidationResult {
  const { value, locate, diagnostics } = parseSource(source);
  if (!value) return { ok: false, diagnostics };

  const parsed = manifestSchema.safeParse(value);
  if (!parsed.success) return { ok: false, diagnostics: fromZod(parsed.error, locate) };

  const found = [...checkSemantics(parsed.data, locate), ...advise(parsed.data, locate)];
  if (blocks(found)) return { ok: false, diagnostics: found };

  return { ok: true, diagnostics: found, manifest: normalize(parsed.data) };
}
