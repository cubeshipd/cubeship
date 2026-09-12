import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";
import { Wordmark } from "@/components/brand";
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
    ],
    githubUrl,
    // One palette, so nothing offers to switch.
    themeSwitch: { enabled: false },
  };
}
