import { Check, X } from "lucide-react";
import { CodeTerminal, Section } from "./section";

const can = [
  "create a project and an environment",
  "deploy an app, then read how the deploy ended",
  "read its logs and what its container is using",
  "attach a database and set the variables",
  "install a template, and update it when a release comes out",
  "list the servers, the stores and the buckets",
];

const cannot = ["set a container's ceiling", "open a port to the internet"];

export function Agent() {
  return (
    <Section
      id="agent"
      label="Run by you or your agent"
      title="The same API, with an endpoint for an agent."
      lede="/mcp is authenticated with the same key as the dashboard and the CLI. Point Claude Code, Cursor or whatever you run at it, and it does the work of the dashboard without the dashboard."
    >
      <div className="grid gap-10 lg:grid-cols-2">
        <CodeTerminal
          title="mcp.json"
          lang="json"
          // Keys in the green template.yaml's keys are, values in its
          // light blue. The theme gives JSON keys the blue it gives
          // numbers, and this snippet has none.
          colorReplacements={{
            "github-dark": { "#79b8ff": "#85e89d" },
            "github-light": { "#005cc5": "#22863a" },
          }}
          code={`{
  "mcpServers": {
    "cubeship": {
      "type": "http",
      "url": "https://cubeship.example.com/mcp",
      "headers": { "Authorization": "Bearer <your-api-key>" }
    }
  }
}`}
        />
        <div className="grid gap-8 sm:grid-cols-2 lg:grid-cols-1">
          <div>
            <p className="label text-subtle-foreground">An agent can</p>
            <ul className="mt-3 space-y-2 text-sm">
              {can.map((c) => (
                <li key={c} className="flex gap-3">
                  <Check className="mt-0.5 size-4 shrink-0 text-success" />
                  <span>{c}</span>
                </li>
              ))}
            </ul>
          </div>
          <div>
            <p className="label text-subtle-foreground">Two lines it does not cross</p>
            <ul className="mt-3 space-y-2 text-sm">
              {cannot.map((c) => (
                <li key={c} className="flex gap-3">
                  <X className="mt-0.5 size-4 shrink-0 text-magenta" />
                  <span>{c}</span>
                </li>
              ))}
            </ul>
            <p className="mt-4 text-muted-foreground text-sm leading-relaxed">
              Stateless on purpose: every call carries the key, so nothing an agent opened can be
              picked up by another. Those two stay with a person.
            </p>
          </div>
        </div>
      </div>
    </Section>
  );
}
