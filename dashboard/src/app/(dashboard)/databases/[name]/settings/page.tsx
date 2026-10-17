import { redirect } from "next/navigation";

const tabForSettings = {
  backups: "backups",
  access: "access",
  network: "access",
  resources: "resources",
  danger: "resources",
} as const;

export default async function DatabaseSettingsRedirect({
  params,
  searchParams,
}: PageProps<"/databases/[name]/settings">) {
  const [{ name }, query] = await Promise.all([params, searchParams]);
  const requested = typeof query.tab === "string" ? query.tab : "backups";
  const tab = tabForSettings[requested as keyof typeof tabForSettings] ?? "resources";
  redirect(`/databases/${name}?tab=${tab}`);
}
