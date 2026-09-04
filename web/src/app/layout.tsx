import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Cubeship",
  description: "Self-hosted PaaS",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body className="antialiased">{children}</body>
    </html>
  );
}
