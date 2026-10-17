import { redirect } from "next/navigation";

export default async function ProjectSettingsRedirect({
  params,
}: PageProps<"/projects/[project]/settings">) {
  const { project } = await params;
  redirect(`/projects/${project}/production`);
}
