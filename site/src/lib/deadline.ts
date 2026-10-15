// Gives up on a promise after `ms`, for the few reads a page can do without.
// The abandoned promise keeps running, so its eventual rejection is caught
// here: unhandled, Node would report it as a crash-worthy rejection.
export function withDeadline<T>(promise: Promise<T>, ms: number, what: string): Promise<T> {
  promise.catch(() => {});
  let timer: ReturnType<typeof setTimeout> | undefined;
  const expired = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error(`${what} took longer than ${ms}ms`)), ms);
  });
  return Promise.race([promise, expired]).finally(() => clearTimeout(timer));
}
