import type { NextConfig } from "next";

// The dashboard runs as its own container, from a Next server.
//
// It used to be a static export compiled into the daemon, and that
// bought one binary at the cost of every route being static: no
// [dynamic] segments, so whatever identified a resource travelled in the
// query string. Four levels deep — an organization, a project, an
// environment, an app; or a provider, a zone, a record — that stopped
// being a constraint worth paying and started being the shape of the
// product bending around a build flag.
//
// standalone is what makes the image small enough for that to be a fair
// trade: Next traces exactly the files the server needs and writes them
// beside a server.js, so the image carries neither node_modules nor the
// toolchain that built it.
const dev = process.env.NODE_ENV === "development";

const nextConfig: NextConfig = {
  output: dev ? undefined : "standalone",
  devIndicators: false,
  turbopack: {
    root: dev ? process.cwd() : undefined,
  },

  // The daemon is in front of this in every mode — it serves /api and
  // proxies everything else here — so this rewrite exists only for
  // `make web-dev`, where the two are reached at different ports and
  // the browser talks to Next directly.
  async rewrites() {
    if (!dev) return [];
    return [{ source: "/api/:path*", destination: "http://127.0.0.1:3000/api/:path*" }];
  },
};

export default nextConfig;
