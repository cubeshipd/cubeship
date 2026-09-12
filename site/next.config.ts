import { createMDX } from "fumadocs-mdx/next";
import type { NextConfig } from "next";

const withMDX = createMDX();

// The site runs as a container from a Next server (Dockerfile.site).
// standalone traces exactly the files the server needs, so the image
// carries neither node_modules nor the toolchain that built it.
const dev = process.env.NODE_ENV === "development";

const config: NextConfig = {
  output: dev ? undefined : "standalone",
  reactStrictMode: true,
  // `curl -fsSL https://cubeship.dev/install.sh | sh` is what install.sh
  // and the CLI print, so the script is served from here — from the
  // repository's master, so the site never carries a stale copy of it.
  // A rewrite rather than a redirect: `sh` never sees a Location header.
  async rewrites() {
    return [
      {
        source: "/install.sh",
        destination: "https://raw.githubusercontent.com/cubeshipd/cubeship/master/install.sh",
      },
    ];
  },
};

export default withMDX(config);
