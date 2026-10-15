import { ApiError } from "@/lib/api";

// message pulls the text out of whatever a failed call threw. Every
// error the daemon returns is plain text, so there is nothing to parse.
export function message(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return String(err);
}
