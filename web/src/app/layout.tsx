import type { Metadata } from "next";
import localFont from "next/font/local";
import { ThemeBoot } from "@/components/theme";
import { Toaster } from "@/components/ui/sonner";
import "./globals.css";

// The faces are vendored under src/fonts rather than fetched from
// Google at build time: `make web` already needs the network for
// `pnpm install`, and a second place a build can fail is one too many.
const chakra = localFont({
  src: [
    { path: "../fonts/ChakraPetch-400.woff2", weight: "400", style: "normal" },
    { path: "../fonts/ChakraPetch-500.woff2", weight: "500", style: "normal" },
    { path: "../fonts/ChakraPetch-600.woff2", weight: "600", style: "normal" },
    { path: "../fonts/ChakraPetch-700.woff2", weight: "700", style: "normal" },
  ],
  variable: "--font-chakra",
  display: "swap",
  fallback: ["ui-sans-serif", "system-ui", "sans-serif"],
});

const jbmono = localFont({
  src: "../fonts/JetBrainsMono-400-700.woff2",
  weight: "400 700",
  variable: "--font-jbmono",
  display: "swap",
  fallback: ["ui-monospace", "SFMono-Regular", "monospace"],
});

export const metadata: Metadata = {
  title: "Cubeship",
  description: "Self-hosted PaaS",
};

// **Every palette is dark**, so `dark` is on <html> rather than left to
// the system: the shadcn primitives carry `dark:` rules and a visitor
// whose OS is set to light would otherwise get half of them. Which of
// the palettes is on `data-theme`, written before the first paint by
// ThemeBoot — an effect runs after it, which is the flash.
export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={`dark ${chakra.variable} ${jbmono.variable}`}>
      <head>
        <ThemeBoot />
      </head>
      <body className="font-sans antialiased">
        {children}
        <Toaster theme="dark" position="bottom-right" />
      </body>
    </html>
  );
}
