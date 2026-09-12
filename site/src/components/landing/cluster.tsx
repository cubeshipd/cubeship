import { Comment, Prompt, Section, Terminal } from "./section";

const points = [
  {
    title: "The new machine dials home",
    body: "It publishes no port to the internet, serves no dashboard and needs no certificate of its own. A box behind NAT can report in and be told what to run.",
  },
  {
    title: "Every name still arrives at your instance",
    body: "One DNS record, one certificate store. Moving an app between machines touches neither.",
  },
  {
    title: "A private network between them",
    body: "Encrypted, and container names mean the same thing on every machine — an app on one reaches a database on another by the name it already had.",
  },
  {
    title: "Scaling takes effect at once",
    body: "Four copies, spread. Or hand the count over: a CPU target, a minimum and a maximum.",
  },
];

export function Cluster() {
  return (
    <Section
      id="cluster"
      label="One VPS or a whole cluster"
      title="Add a server, and the instance becomes a cluster."
    >
      <div className="grid gap-10 lg:grid-cols-2">
        <Terminal>
          <Prompt>cubeship server add eu-1</Prompt>
          <Comment># prints the command to run on the new box, credential in it</Comment>
          {"\n"}
          <Prompt>cubeship app place api --on eu-1</Prompt>
          <Prompt>cubeship app place api --replicas 4</Prompt>
          <Prompt>cubeship app place api --everywhere</Prompt>
          {"\n"}
          <Prompt>cubeship app autoscale api --min 2 --max 8 --cpu 70</Prompt>
        </Terminal>
        <ul className="grid gap-6 sm:grid-cols-2 lg:grid-cols-1">
          {points.map((p) => (
            <li key={p.title} className="border-border border-l pl-4">
              <h3 className="font-semibold text-base">{p.title}</h3>
              <p className="mt-1.5 text-muted-foreground text-sm leading-relaxed">{p.body}</p>
            </li>
          ))}
        </ul>
      </div>
    </Section>
  );
}
