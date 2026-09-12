import type { Metadata } from "next";
import { Agent } from "@/components/landing/agent";
import { Cluster } from "@/components/landing/cluster";
import { Deploy } from "@/components/landing/deploy";
import { Features } from "@/components/landing/features";
import { Footer } from "@/components/landing/footer";
import { Hero } from "@/components/landing/hero";
import { Privacy } from "@/components/landing/privacy";
import { Screens } from "@/components/landing/screens";

export const metadata: Metadata = {
  title: { absolute: "Cubeship — self-hosted PaaS for your own servers" },
  alternates: { canonical: "/" },
};

export default function HomePage() {
  return (
    <main className="flex-1">
      <Hero />
      <Features />
      <Screens />
      <Deploy />
      <Agent />
      <Cluster />
      <Privacy />
      <Footer />
    </main>
  );
}
