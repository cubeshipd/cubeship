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
  title: { default: "Cubeship — self-hosted PaaS for your own servers", template: "%s · Cubeship" },
  description,
  applicationName: "Cubeship",
  keywords: [
    "self-hosted PaaS",
    "deploy to your own VPS",
    "Docker deploy",
    "Heroku alternative",
    "Coolify alternative",
    "zero-downtime deploys",
    "MCP server for deployments",
    "Postgres backups",
  ],
  authors: [{ name: "Cubeship", url: siteUrl }],
  creator: "Cubeship",
  category: "technology",
  alternates: { canonical: "/" },
  robots: {
    index: true,
    follow: true,
    googleBot: { index: true, follow: true, "max-image-preview": "large" },
  },
  openGraph: {
    type: "website",
    url: siteUrl,
    siteName: "Cubeship",
    locale: "en_US",
    title: "Cubeship — self-hosted PaaS for your own servers",
    description,
  },
  twitter: { card: "summary_large_image", title: "Cubeship", description },
};

// What a search engine is told the site is, beyond what it can read.
const structuredData = {
  "@context": "https://schema.org",
  "@graph": [
    {
      "@type": "WebSite",
      "@id": `${siteUrl}/#website`,
      url: siteUrl,
      name: "Cubeship",
      description,
    },
    {
      "@type": "SoftwareApplication",
      "@id": `${siteUrl}/#software`,
      name: "Cubeship",
      applicationCategory: "DeveloperApplication",
      operatingSystem: "Linux",
      description,
      url: siteUrl,
      license: "https://www.apache.org/licenses/LICENSE-2.0",
      offers: { "@type": "Offer", price: "0", priceCurrency: "USD" },
      softwareHelp: { "@type": "CreativeWork", url: `${siteUrl}/docs` },
      codeRepository: "https://github.com/cubeshipd/cubeship",
    },
  ],
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
        <script type="application/ld+json">{JSON.stringify(structuredData)}</script>
        <RootProvider theme={{ enabled: false }}>{children}</RootProvider>
      </body>
    </html>
  );
}
