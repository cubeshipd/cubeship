import type { NextConfig } from "next";

// The dashboard is compiled into the daemon, so it exports to static
// files: a Cubeship box runs one Go binary and no Node process, and
// every server feature Next offers would be duplicating the daemon
// sitting right behind it.
//
// A static export has nothing to fall back on, so every route here is a
// real static route and whatever identifies a resource travels in the
// query string. That is why there are no [dynamic] segments.
const dev = process.env.NODE_ENV === "development";

const nextConfig: NextConfig = {
  output: dev ? undefined : "export",
  images: { unoptimized: true },

  // `make web-dev` serves this on its own port, so /api has to be sent
  // to the daemon. In a build there is no proxy and none is needed:
  // the daemon serves both.
  async rewrites() {
    if (!dev) return [];
    return [{ source: "/api/:path*", destination: "http://127.0.0.1:3000/api/:path*" }];
  },
};

export default nextConfig;
