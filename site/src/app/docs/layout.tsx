import { DocsLayout } from "fumadocs-ui/layouts/docs";
import { GitHubStars } from "@/components/github-stars";
import { baseOptions } from "@/lib/layout.shared";
import { source } from "@/lib/source";

export default function Layout({ children }: LayoutProps<"/docs">) {
  return (
    // The header's links are the header's; the sidebar is the tree.
    <DocsLayout
      tree={source.getPageTree()}
      {...baseOptions()}
      links={[]}
      sidebar={{ footer: <GitHubStars /> }}
    >
      {children}
    </DocsLayout>
  );
}
