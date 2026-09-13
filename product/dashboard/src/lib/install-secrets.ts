// The secrets an install or an update generated, handed from the form
// that started it to the page that shows them once.
//
// In memory, not in sessionStorage or the URL: the hand-off is a client
// navigation, so a module outlives it, and nothing is written anywhere a
// script or the history could read later. A reload loses them — the same
// as closing the page they are shown on, which is already the end of them.
const pending = new Map<number, Record<string, string>>();

export function handOverSecrets(installId: number, secrets: Record<string, string> | undefined) {
  if (secrets && Object.keys(secrets).length > 0) pending.set(installId, secrets);
}

// takeSecrets returns them once and forgets them.
export function takeSecrets(installId: number): Record<string, string> {
  const secrets = pending.get(installId) ?? {};
  pending.delete(installId);
  return secrets;
}
