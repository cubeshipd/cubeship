import { RootProvider } from "fumadocs-ui/provider/next";
import type { Metadata, Viewport } from "next";
import localFont from "next/font/local";
import { siteUrl } from "@/lib/shared";
import "./global.css";

// The dashboard's own two faces, vendored the same way it vendors them:
// a build that needs the network for a font is a second place it can
// fail.
const chakra = localFont({
  src: [
    { path: "../fonts/ChakraPetch-400.woff2", weight: "400", style: "normal" },
    { path: "../fonts/ChakraPetch-500.woff2", weight: "500", style: "normal" },
    { path: "../fonts/ChakraPetch-600.woff2", weight: "600", style: "normal" },
    { path: "../fonts/ChakraPetch-700.woff2", weight: "700", style: "normal" },
  ],
  variable: "--font-chakra",
  display: "swap",
});

const jbmono = localFont({
  src: "../fonts/JetBrainsMono-400-700.woff2",
  variable: "--font-jbmono",
  display: "swap",
});

const description = "Self-hosted PaaS — one VPS or a whole cluster, run by you or your agent.";

// The icons and the social card are files beside this one, found by
// name; none of them is listed here.
export const metadata: Metadata = {
  metadataBase: new URL(siteUrl),
  title: { default: "Cubeship", template: "%s · Cubeship" },
  description,
  applicationName: "Cubeship",
  openGraph: { title: "Cubeship", description, siteName: "Cubeship", type: "website" },
};

export const viewport: Viewport = { themeColor: "#05070a", colorScheme: "dark" };

// One palette, the dashboard's: `dark` is on <html> for the primitives
// that carry `dark:` rules, and next-themes is off so nothing offers
// to switch.
export default function Layout({ children }: LayoutProps<"/">) {
  return (
    <html
      lang="en"
      className={`dark ${chakra.variable} ${jbmono.variable}`}
      suppressHydrationWarning
    >
      <body className="flex min-h-screen flex-col font-sans antialiased">
        <RootProvider theme={{ enabled: false }}>{children}</RootProvider>
      </body>
    </html>
  );
}
