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

// The dashboard can stand on invented data with no daemon behind it —
// `make dashboard-preview`, which sets this. See src/lib/mock.
const preview = process.env.NEXT_PUBLIC_CUBESHIP_MOCK === "1";
const publicDemo = process.env.NEXT_PUBLIC_CUBESHIP_DEMO === "1";
if (publicDemo && !preview) throw new Error("The public demo requires preview data.");

const nextConfig: NextConfig = {
  basePath: publicDemo ? "/demo" : "",
  distDir: publicDemo ? ".next-demo" : ".next",
  output: dev ? undefined : "standalone",
  devIndicators: false,
  turbopack: {
    root: dev ? process.cwd() : undefined,
    // **The fixtures are aliased away unless the preview asked for
    // them.** The import in lib/api.ts is dynamic and behind a flag, so
    // nothing ever loads it in a normal build — and a dead branch is
    // not the same promise as an absent file: the chunk was emitted
    // anyway, which put an invented instance, invented hostnames and
    // things shaped like keys inside the image people run. This is what
    // makes "not in the build" true rather than nearly true.
    resolveAlias: preview ? undefined : { "@/lib/mock": "./src/lib/mock/absent.ts" },
  },

  // The daemon is in front of this in every mode — it serves /api and
  // proxies everything else here — so this rewrite exists only for
  // `make dashboard-dev`, where the two are reached at different ports and
  // the browser talks to Next directly.
  async rewrites() {
    if (!dev || preview) return [];
    return [{ source: "/api/:path*", destination: "http://127.0.0.1:3000/api/:path*" }];
  },
  async headers() {
    if (!publicDemo) return [];
    return [
      {
        source: "/:path*",
        headers: [
          { key: "X-Robots-Tag", value: "noindex, nofollow" },
          {
            key: "Content-Security-Policy",
            value:
              "frame-ancestors 'self'; form-action 'none'; object-src 'none'; base-uri 'self'; connect-src 'self'",
          },
        ],
      },
    ];
  },
};

export default nextConfig;
