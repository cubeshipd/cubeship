import type { App } from "@/lib/api";

// DEFAULT_PORT is what the daemon serves a name on when nobody said
// otherwise. It is app.DefaultPort.
export const DEFAULT_PORT = 8080;

// A public name is served on the instance's one scheme; https until the
// settings say TLS is off.
export function publicUrl(host: string, tlsEnabled: boolean | undefined): string {
  return `${tlsEnabled === false ? "http" : "https"}://${host}`;
}

// Plain http: nothing terminates TLS on the internal network. With
// several ports behind its names, the first one's.
export function internalUrl(app: App): string {
  return `http://${app.internal_host}:${app.domains[0]?.port || DEFAULT_PORT}`;
}
