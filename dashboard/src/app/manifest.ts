import type { MetadataRoute } from "next";
import { DEMO_PATH } from "@/lib/demo";

// Picked up by filename, like the icons beside it: Next writes the
// <link rel="manifest"> and serves this at /manifest.webmanifest.
export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "Cubeship",
    short_name: "Cubeship",
    description: "Self-hosted PaaS for one VPS",
    start_url: `${DEMO_PATH}/`,
    display: "standalone",
    background_color: "#05070a",
    theme_color: "#05070a",
    icons: [
      { src: `${DEMO_PATH}/logo/icon-192.png`, sizes: "192x192", type: "image/png" },
      { src: `${DEMO_PATH}/logo/icon-512.png`, sizes: "512x512", type: "image/png" },
      {
        src: `${DEMO_PATH}/logo/icon-512.png`,
        sizes: "512x512",
        type: "image/png",
        purpose: "maskable",
      },
    ],
  };
}
