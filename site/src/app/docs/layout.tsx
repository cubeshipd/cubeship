import { DocsLayout } from "fumadocs-ui/layouts/docs";
import { baseOptions } from "@/lib/layout.shared";
import { githubUrl } from "@/lib/shared";
import { source } from "@/lib/source";

export default function Layout({ children }: LayoutProps<"/docs">) {
  return (
    // The header's links are the header's; the sidebar is the tree.
    <DocsLayout tree={source.getPageTree()} {...baseOptions()} links={[]} githubUrl={githubUrl}>
      {children}
    </DocsLayout>
  );
}
