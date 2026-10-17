import { redirect } from "next/navigation";

export default async function EnvironmentSettingsRedirect({
  params,
}: PageProps<"/projects/[project]/[env]/settings">) {
  const { project, env } = await params;
  redirect(`/projects/${project}/${env}`);
}
