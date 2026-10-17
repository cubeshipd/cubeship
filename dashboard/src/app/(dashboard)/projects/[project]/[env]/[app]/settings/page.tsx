import { redirect } from "next/navigation";

const tabForSettings = {
  network: "network",
  source: "build",
  deploy: "build",
  resources: "resources",
  volumes: "resources",
  danger: "resources",
} as const;

export default async function AppSettingsRedirect({
  params,
  searchParams,
}: PageProps<"/projects/[project]/[env]/[app]/settings">) {
  const [{ project, env, app }, query] = await Promise.all([params, searchParams]);
  const requested = typeof query.tab === "string" ? query.tab : "network";
  const tab = tabForSettings[requested as keyof typeof tabForSettings] ?? "network";
  redirect(`/projects/${project}/${env}/${app}?tab=${tab}`);
}
