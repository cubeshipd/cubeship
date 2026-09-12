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

export function fail(error: unknown): Response {
  if (error instanceof HttpError) {
    return json(
      { error: { code: error.code, message: error.message }, ...error.extra },
      { status: error.status },
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

// context is optional so a test can call a handler with a Request alone.
type Handler = (
  request: Request,
  context?: { params: Promise<Record<string, string>> },
) => Promise<Response>;

export function route(handler: Handler): Handler {
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
