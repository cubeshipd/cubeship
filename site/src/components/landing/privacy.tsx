import { Section } from "./section";

const leaves = [
  {
    what: "Installing and upgrading",
    why: "pulls two images from ghcr.io, and asks GitHub which release is newest so it can pin an exact one.",
  },
  {
    what: "The running daemon",
    why: "asks GitHub which releases exist when an admin opens the update screen. A plain GET, no credential, nothing about your instance in it.",
  },
  {
    what: "Let's Encrypt",
    why: "for certificates — and only once you have given the instance a domain.",
  },
];

export function Privacy() {
  return (
    <Section
      id="privacy"
      label="What reaches the internet"
      title="There is no telemetry."
      lede="Nothing reports what you deploy, how much you deploy, or that this instance exists. No analytics in the dashboard, no account with anybody. Three things leave the box, and all three are yours to look at."
    >
      <ol className="grid gap-px border border-border bg-border md:grid-cols-3">
        {leaves.map((l, i) => (
          <li key={l.what} className="bg-background p-6">
            <span className="label text-subtle-foreground">0{i + 1}</span>
            <h3 className="mt-3 font-semibold">{l.what}</h3>
            <p className="mt-2 text-muted-foreground text-sm leading-relaxed">{l.why}</p>
          </li>
        ))}
      </ol>
    </Section>
  );
}
