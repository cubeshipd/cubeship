import type { Metadata } from "next";
import { Agent } from "@/components/landing/agent";
import { Cluster } from "@/components/landing/cluster";
import { Deploy } from "@/components/landing/deploy";
import { Features } from "@/components/landing/features";
import { Footer } from "@/components/landing/footer";
import { Hero } from "@/components/landing/hero";
import { Privacy } from "@/components/landing/privacy";
import { Screens } from "@/components/landing/screens";
import { TemplatesStrip } from "@/components/landing/templates-strip";

export const metadata: Metadata = {
  title: { absolute: "Cubeship — self-hosted PaaS for your own servers" },
  alternates: { canonical: "/" },
};

// TemplatesStrip reads the catalog, so the page it sits on can never be
// static — the price of showing live rows on the landing page.
export const dynamic = "force-dynamic";

export default function HomePage() {
  return (
    <main className="flex-1">
      <Hero />
      <Features />
      <Screens />
      <Deploy />
      <Agent />
      <Cluster />
      <TemplatesStrip />
      <Privacy />
      <Footer />
    </main>
  );
}
