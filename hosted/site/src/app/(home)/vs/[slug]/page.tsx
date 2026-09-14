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
    <main id="main-content" className="editorial-page comparison-page flex-1">
      <section className="comparison-hero">
        <div className="site-container comparison-hero-grid">
          <div>
            <p className="section-kicker">A practical comparison</p>
            <h1>{c.title}</h1>
          </div>
          <div className="comparison-promise hud-frame">
            <strong>Cubeship is 100% free.</strong>
            <p>Every feature, every server, every user. No plans, no cloud.</p>
            <p className="comparison-cost">{c.cost}</p>
          </div>
        </div>
      </section>

      <section className="comparison-evidence">
        <div className="site-container comparison-content">
          <div className="comparison-table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Feature</th>
                  <th>Cubeship</th>
                  <th>{c.name}</th>
                </tr>
              </thead>
              {c.groups.map((g) => (
                <tbody key={g.title}>
                  <tr>
                    <th colSpan={3} className="comparison-group">
                      {g.title}
                    </th>
                  </tr>
                  {g.rows.map((r) => (
                    <tr key={r.feature}>
                      <td>{r.feature}</td>
                      <td>
                        <Mark on={r.cubeship} />
                      </td>
                      <td>
                        <Mark on={r.other} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              ))}
            </table>
          </div>
          <p className="comparison-checked">
            Checked against {c.checked}. Wrong or out of date?{" "}
            <a href="https://github.com/cubeshipd/cubeship/issues" className="inline-link">
              Say so.
            </a>
          </p>
          <div className="comparison-cta">
            <div>
              <h2>One VPS. Your entire platform.</h2>
              <p>
                Install Cubeship and keep the server, data, and deployment path under your control.
              </p>
            </div>
            <div className="comparison-cta-actions">
              <Link href="/docs/getting-started/install" className="button-primary neon-edge">
                Install Cubeship <ArrowRight className="size-4" />
              </Link>
              <Link href="/docs" className="comparison-secondary-action">
                Read the docs
              </Link>
            </div>
          </div>
        </div>
      </section>
      <Footer />
    </main>
  );
}
