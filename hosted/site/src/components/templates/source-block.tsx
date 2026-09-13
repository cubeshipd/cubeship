import { ServerCodeBlock } from "fumadocs-ui/components/codeblock.rsc";
import { ExternalLink } from "lucide-react";

// Highlighted on the server with the docs' own themes, so the file
// arrives coloured rather than repainting after load.
export function SourceBlock({ source, fileUrl }: { source: string; fileUrl: string }) {
  return (
    <ServerCodeBlock
      code={source}
      lang="yaml"
      themes={{ light: "github-light", dark: "github-dark" }}
      codeblock={{
        title: (
          <span className="flex w-full items-center justify-between gap-3">
            <span>template.yaml</span>
            <a
              href={fileUrl}
              target="_blank"
              rel="noreferrer"
              aria-label="template.yaml on GitHub, at this release"
              title="template.yaml on GitHub, at this release"
              className="text-fd-muted-foreground transition-colors hover:text-primary"
            >
              <ExternalLink className="size-3.5" />
            </a>
          </span>
        ),
      }}
    />
  );
}
