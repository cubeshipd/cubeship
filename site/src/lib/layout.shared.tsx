import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";
import { Wordmark } from "@/components/brand";
import { GitHubStars } from "@/components/github-stars";
import { githubUrl } from "./shared";

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: <Wordmark />,
      url: "/",
    },
    links: [
      { text: "Docs", url: "/docs" },
      { text: "Changelog", url: `${githubUrl}/blob/master/CHANGELOG.md` },
      // The star count in place of the preset's plain icon; the docs
      // sidebar keeps the icon through githubUrl on its own layout.
      { type: "custom", secondary: true, children: <GitHubStars /> },
    ],
    // One palette, so nothing offers to switch.
    themeSwitch: { enabled: false },
  };
}
