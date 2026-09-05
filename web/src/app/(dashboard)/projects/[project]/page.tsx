import { redirect } from "next/navigation";

// A project opens on the environment it always has and cannot lose.
//
// A redirect rather than a page of its own, so there is one screen for
// "a project's apps" instead of two that have to stay identical — and so
// the address bar says which environment you are looking at even when
// you did not pick one.
export default async function Project({ params }: { params: Promise<{ project: string }> }) {
  const { project } = await params;
  redirect(`/projects/${project}/production`);
}
