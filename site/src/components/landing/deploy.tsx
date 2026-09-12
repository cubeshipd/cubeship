import { Comment, Prompt, Section, Terminal } from "./section";

export function Deploy() {
  return (
    <Section
      id="deploy"
      label="Deploy an app"
      title="Three ways in. None of them needs a YAML file."
      lede="An app is named project/environment/app, which is also its path in the registry. Create one empty; give it a name on the internet when it has business answering there."
    >
      <div className="grid gap-6 lg:grid-cols-3">
        <Terminal title="Push an image">
          <Prompt>docker login registry.example.com</Prompt>
          <Prompt>docker push registry.example.com/shop/production/api:latest</Prompt>
          <Comment># the push is the deploy</Comment>
        </Terminal>
        <Terminal title="Run one from anywhere">
          <Prompt>cubeship app create api --source external --image nginx</Prompt>
          <Prompt>cubeship app deploy api --tag 1.27</Prompt>
          <Comment># Docker Hub, GHCR, ECR — no domain, no registry needed</Comment>
        </Terminal>
        <Terminal title="Build from a repository">
          <Comment># connect GitHub once; a push builds and deploys</Comment>
          <Comment># a Dockerfile if you wrote one — or none at all</Comment>
          <Prompt>git push origin main</Prompt>
        </Terminal>
      </div>
    </Section>
  );
}
