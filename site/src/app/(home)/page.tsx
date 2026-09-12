import { Agent } from "@/components/landing/agent";
import { Cluster } from "@/components/landing/cluster";
import { Deploy } from "@/components/landing/deploy";
import { Features } from "@/components/landing/features";
import { Footer } from "@/components/landing/footer";
import { Hero } from "@/components/landing/hero";
import { Privacy } from "@/components/landing/privacy";

export default function HomePage() {
  return (
    <main className="flex-1">
      <Hero />
      <Features />
      <Deploy />
      <Agent />
      <Cluster />
      <Privacy />
      <Footer />
    </main>
  );
}
