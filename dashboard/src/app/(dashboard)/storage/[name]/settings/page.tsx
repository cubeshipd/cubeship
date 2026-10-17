import { redirect } from "next/navigation";

export default async function ObjectStoreSettingsPage({
  params,
  searchParams,
}: PageProps<"/storage/[name]/settings">) {
  const { name } = await params;
  const { tab } = await searchParams;
  redirect(
    `/storage/${encodeURIComponent(name)}?tab=${tab === "resources" ? "resources" : "access"}`,
  );
}
