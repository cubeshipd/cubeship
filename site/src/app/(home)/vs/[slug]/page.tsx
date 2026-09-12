import { ArrowRight, Check, Minus } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { Footer } from "@/components/landing/footer";
import { comparison, comparisons } from "@/lib/comparisons";

export function generateStaticParams() {
  return comparisons.map((c) => ({ slug: c.slug }));
}

export async function generateMetadata(props: PageProps<"/vs/[slug]">): Promise<Metadata> {
  const { slug } = await props.params;
  const c = comparison(slug);
  if (!c) notFound();
  return {
    title: { absolute: c.title },
    description: c.description,
    alternates: { canonical: `/vs/${c.slug}` },
    openGraph: { title: c.title, description: c.description, type: "article" },
  };
}

function Mark({ on }: { on: boolean }) {
  return on ? (
    <Check className="mx-auto size-4 text-success" aria-label="yes" />
  ) : (
    <Minus className="mx-auto size-4 text-subtle-foreground" aria-label="no" />
  );
}

export default async function ComparePage(props: PageProps<"/vs/[slug]">) {
  const { slug } = await props.params;
  const c = comparison(slug);
  if (!c) notFound();

  return (
    <main className="flex-1">
      <section className="border-border border-b">
        <div className="mx-auto max-w-4xl px-6 pt-20 pb-14 text-center">
          <p className="label text-primary">Compared</p>
          <h1 className="mt-4 font-semibold text-4xl tracking-tight sm:text-5xl">{c.title}</h1>
          <p className="hud-frame mx-auto mt-8 inline-block border border-border bg-card px-5 py-3 font-mono text-sm">
            <span className="text-success">Cubeship is 100% free.</span>{" "}
            <span className="text-muted-foreground">
              Every feature, every server, every user. No plans, no cloud.
            </span>
          </p>
          <p className="mt-4 text-muted-foreground text-sm">{c.cost}</p>
        </div>
      </section>

      <section className="border-border border-b">
        <div className="mx-auto max-w-4xl px-6 py-16">
          <table className="w-full border-collapse">
            <thead>
              <tr className="label text-subtle-foreground">
                <th className="py-3 text-left font-normal">Feature</th>
                <th className="w-28 py-3 text-center font-normal text-primary">Cubeship</th>
                <th className="w-28 py-3 text-center font-normal">{c.name}</th>
              </tr>
            </thead>
            {c.groups.map((g) => (
              <tbody key={g.title}>
                <tr>
                  <th
                    colSpan={3}
                    className="label border-border border-t pt-6 pb-2 text-left text-primary"
                  >
                    <span className="mr-2 inline-block h-3 w-0.5 bg-primary align-middle" />
                    {g.title}
                  </th>
                </tr>
                {g.rows.map((r) => (
                  <tr key={r.feature} className="border-border border-t">
                    <td className="py-3 text-sm">{r.feature}</td>
                    <td className="py-3">
                      <Mark on={r.cubeship} />
                    </td>
                    <td className="py-3">
                      <Mark on={r.other} />
                    </td>
                  </tr>
                ))}
              </tbody>
            ))}
          </table>
          <p className="mt-6 text-subtle-foreground text-xs">
            Checked against {c.checked}. Wrong or out of date?{" "}
            <a
              href="https://github.com/cubeshipd/cubeship/issues"
              className="underline hover:text-primary"
            >
              Say so.
            </a>
          </p>
          <div className="mt-12 flex flex-wrap justify-center gap-4">
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
        </div>
      </section>
      <Footer />
    </main>
  );
}
