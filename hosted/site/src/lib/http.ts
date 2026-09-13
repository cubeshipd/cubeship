export class HttpError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly extra?: Record<string, unknown>,
  ) {
    super(message);
  }
}

export function json(data: unknown, init?: ResponseInit): Response {
  return Response.json(data, init);
}

// Node/pg errno codes for a connection that never opened, plus 57014
// (query_canceled), the SQLSTATE our own statement_timeout comes back
// as. drizzle wraps the real error in a DrizzleQueryError and puts it
// on `.cause`, so isDatabaseUnavailable walks that chain rather than
// checking `error` alone.
const CONNECTION_ERROR_CODES = new Set([
  "ECONNREFUSED",
  "ETIMEDOUT",
  "EHOSTUNREACH",
  "ENETUNREACH",
  "ENOTFOUND",
  "57014",
]);

// pg's own connect timeout and pg-pool's checkout timeout throw a plain
// Error with one of these messages and no code at all — there is
// nothing else to match them on.
const CONNECTION_TIMEOUT_MESSAGE =
  /timeout expired|connection terminated due to connection timeout|timeout exceeded when trying to connect|query read timeout/i;

function isDatabaseUnavailable(error: unknown, depth = 0): boolean {
  if (!(error instanceof Error) || depth > 3) return false;
  const code = (error as NodeJS.ErrnoException).code;
  if (code && CONNECTION_ERROR_CODES.has(code)) return true;
  if (CONNECTION_TIMEOUT_MESSAGE.test(error.message)) return true;
  return isDatabaseUnavailable((error as { cause?: unknown }).cause, depth + 1);
}

export function fail(error: unknown): Response {
  if (error instanceof HttpError) {
    return json(
      { error: { code: error.code, message: error.message }, ...error.extra },
      { status: error.status },
    );
  }
  if (isDatabaseUnavailable(error)) {
    console.error("database unavailable:", (error as Error).message);
    return json(
      { error: { code: "unavailable", message: "the template registry is unavailable right now" } },
      { status: 503 },
    );
  }
  // Anything unplanned is ours, and its message is not the caller's business.
  console.error(error);
  return json(
    { error: { code: "internal", message: "something went wrong here" } },
    {
      status: 500,
    },
  );
}

// Params is a generic so a catch-all segment (string[]) types the same
// way a plain one (string) does. context is optional so a test can call
// a handler with a Request alone.
type Handler<Params extends Record<string, string | string[]> = Record<string, string>> = (
  request: Request,
  context?: { params: Promise<Params> },
) => Promise<Response>;

export function route<Params extends Record<string, string | string[]> = Record<string, string>>(
  handler: Handler<Params>,
): Handler<Params> {
  return async (request, context) => {
    try {
      return await handler(request, context);
    } catch (error) {
      return fail(error);
    }
  };
}

export async function readText(request: Request, limit = 256 * 1024): Promise<string> {
  const text = await request.text();
  if (text.length > limit) {
    throw new HttpError(
      413,
      "too_large",
      `a template file is at most ${Math.floor(limit / 1024)} KB`,
    );
  }
  return text;
}
