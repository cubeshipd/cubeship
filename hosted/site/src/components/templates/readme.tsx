import Markdown, { defaultUrlTransform } from "react-markdown";
import rehypeRaw from "rehype-raw";
import rehypeSanitize from "rehype-sanitize";
import remarkGfm from "remark-gfm";
import { resolveReadmeUrl } from "./readme-urls";

// Rendered on the server from text a stranger wrote. Raw HTML is parsed
// so a centred logo reads as a logo, then sanitized with GitHub's own
// allowlist before anything reaches the page: no scripts, no styles,
// no event handlers.
export function Readme({
  source,
  owner,
  repo,
  commit,
}: {
  source: string;
  owner: string;
  repo: string;
  commit: string;
}) {
  return (
    <div className="prose max-w-none text-sm">
      <Markdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeRaw, rehypeSanitize]}
        urlTransform={(url, key) =>
          defaultUrlTransform(
            resolveReadmeUrl(url, key === "src" ? "image" : "link", { owner, repo, commit }),
          )
        }
      >
        {source}
      </Markdown>
    </div>
  );
}
