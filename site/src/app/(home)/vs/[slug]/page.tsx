import { ArrowRight, Check } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { Footer } from "@/components/landing/footer";
import { Section } from "@/components/landing/section";
import { cn } from "@/lib/cn";
import { type Cell, comparison, comparisons } from "@/lib/comparisons";

export function generateStaticParams() {
  return comparisons.map((c) => ({ slug: c.slug }));
}

export async function generateMetadata(props: PageProps<"/vs/[slug]">): Promise<Metadata> {
  const { slug } = await props.params;
  const c = comparison(slug);
  if (!c) notFound();
  return {
    title: { absolute: `${c.title} — an honest comparison` },
    description: c.description,
    alternates: { canonical: `/vs/${c.slug}` },
    openGraph: { title: c.title, description: c.description, type: "article" },
  };
}

function Value({ cell }: { cell: Cell }) {
  return (
    <span
      className={cn(
        "text-sm leading-relaxed",
        cell.good ? "text-foreground" : "text-muted-foreground",
      )}
    >
      {cell.good ? <Check className="mr-1.5 inline size-3.5 text-success align-[-2px]" /> : null}
      {cell.text}
    </span>
  );
}

export default async function ComparePage(props: PageProps<"/vs/[slug]">) {
  const { slug } = await props.params;
  const c = comparison(slug);
  if (!c) notFound();

  return (
    <main className="flex-1">
      <section className="border-border border-b">
        <div className="mx-auto max-w-6xl px-6 pt-20 pb-16">
          <p className="label text-primary">Compared</p>
          <h1 className="mt-4 font-semibold text-4xl tracking-tight sm:text-5xl">{c.title}</h1>
          <p className="mt-6 max-w-3xl text-lg text-muted-foreground leading-relaxed">{c.intro}</p>
          <p className="mt-4 text-subtle-foreground text-xs">
            Checked against {c.checked}. Something wrong or out of date?{" "}
            <a
              href="https://github.com/cubeshipd/cubeship/issues"
              className="underline hover:text-primary"
            >
              Say so.
            </a>
          </p>
        </div>
      </section>

      <Section label="Side by side" title="What each one does.">
        <div className="overflow-x-auto border border-border">
          <table className="w-full min-w-[720px] border-collapse text-left">
            <thead>
              <tr className="label border-border border-b text-subtle-foreground">
                <th className="w-[22%] px-4 py-3 font-normal">Feature</th>
                <th className="w-[39%] px-4 py-3 font-normal text-primary">Cubeship</th>
                <th className="w-[39%] px-4 py-3 font-normal">{c.name}</th>
              </tr>
            </thead>
            <tbody>
              {c.rows.map((r) => (
                <tr key={r.feature} className="border-border border-b align-top last:border-b-0">
                  <th className="px-4 py-3 font-medium text-sm">{r.feature}</th>
                  <td className="px-4 py-3">
                    <Value cell={r.cubeship} />
                  </td>
                  <td className="px-4 py-3">
                    <Value cell={r.other} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Section>

      <Section label="The honest part" title={`When ${c.name} is the better choice.`}>
        <ul className="grid gap-4 md:grid-cols-2">
          {c.pickThem.map((p) => (
            <li
              key={p}
              className="border-border border-l pl-4 text-muted-foreground text-sm leading-relaxed"
            >
              {p}
            </li>
          ))}
        </ul>
      </Section>

      <Section label="And the other half" title="When Cubeship is.">
        <ul className="grid gap-4 md:grid-cols-2">
          {c.pickUs.map((p) => (
            <li key={p} className="border-primary border-l pl-4 text-sm leading-relaxed">
              {p}
            </li>
          ))}
        </ul>
        <div className="mt-12 flex flex-wrap gap-4">
          <Link
            href="/docs/getting-started/install"
            className="neon-edge inline-flex items-center gap-2 bg-primary px-5 py-2.5 font-semibold text-primary-foreground text-sm uppercase tracking-[0.18em] transition-colors hover:bg-primary/90"
          >
            Install Cubeship <ArrowRight className="size-4" />
          </Link>
          <Link
            href="/docs"
            className="inline-flex items-center gap-2 border border-border-strong px-5 py-2.5 font-semibold text-foreground text-sm uppercase tracking-[0.18em] transition-colors hover:border-primary hover:text-primary"
          >
            Read the docs
          </Link>
        </div>
      </Section>
      <Footer />
    </main>
  );
}
