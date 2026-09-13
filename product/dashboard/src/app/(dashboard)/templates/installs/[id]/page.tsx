"use client";

import { useQuery } from "@tanstack/react-query";
import { LoaderCircleIcon } from "lucide-react";
import Link from "next/link";
import { use, useEffect, useState } from "react";
import { CopyField } from "@/components/copy-field";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingList } from "@/components/loading";
import { Notice } from "@/components/notice";
import { SectionHeader } from "@/components/section-header";
import { StatusBadge } from "@/components/status-badge";
import { Card, CardContent } from "@/components/ui/card";
import { api, type TemplateInstall } from "@/lib/api";
import { message } from "@/lib/errors";
import { secretsKey } from "../../[owner]/[repo]/page";

// An install, as it goes. Polled while it runs; a failed one has already
// been undone by the daemon, so what is left to say is why.
export default function TemplateInstallPage({ params }: PageProps<"/templates/installs/[id]">) {
  const { id } = use(params);
  const install = useQuery({
    queryKey: ["template-installs", id],
    queryFn: () => api.get<TemplateInstall>(`/template-installs/${id}`),
    refetchInterval: (q) => (q.state.data && q.state.data.status !== "running" ? false : 2000),
  });

  // Read once and forgotten: this page is the one place they are shown.
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  useEffect(() => {
    const key = secretsKey(Number(id));
    const stored = sessionStorage.getItem(key);
    if (stored) {
      setSecrets(JSON.parse(stored));
      sessionStorage.removeItem(key);
    }
  }, [id]);

  if (install.error) return <ErrorAlert error={message(install.error)} />;
  if (!install.data) return <LoadingList rows={4} />;
  const i = install.data;

  return (
    <>
      <div className="mb-6 flex items-center justify-between gap-4">
        <div className="min-w-0">
          <h1 className="truncate text-lg font-semibold">
            {i.owner}/{i.repo}{" "}
            <span className="font-mono text-sm text-muted-foreground">{i.release}</span>
          </h1>
          <p className="font-mono text-xs text-subtle-foreground">
            into {i.project}/{i.environment}
          </p>
        </div>
        <StatusBadge value={i.status} />
      </div>

      {i.status === "running" && (
        <p className="mb-6 flex items-center gap-2 text-sm text-muted-foreground">
          <LoaderCircleIcon className="size-4 animate-spin text-primary" aria-hidden />
          {i.step}
        </p>
      )}
      {i.status === "failed" && (
        <ErrorAlert error={`${i.error} Everything the install created was deleted.`} />
      )}

      {Object.keys(secrets).length > 0 && (
        <>
          <SectionHeader title="Generated secrets" />
          <Notice tone="warning" className="mb-3">
            Shown this once. Keep a copy — they are in the apps' variables, and nowhere else.
          </Notice>
          <Card>
            <CardContent className="grid gap-4 sm:grid-cols-2">
              {Object.entries(secrets).map(([key, value]) => (
                <CopyField key={key} label={key} value={value} masked />
              ))}
            </CardContent>
          </Card>
        </>
      )}

      {i.resources.length > 0 && (
        <>
          <SectionHeader
            title={i.status === "failed" ? "What it had created" : "What it created"}
          />
          <Card className="gap-0 divide-y divide-border py-0">
            {i.resources.map((r) => (
              <div key={`${r.kind}-${r.name}`} className="flex items-center gap-3 px-4 py-2.5">
                <span className="w-24 shrink-0 text-xs uppercase tracking-[0.14em] text-muted-foreground">
                  {r.kind}
                </span>
                {i.status === "failed" ? (
                  <span className="font-mono text-sm text-muted-foreground line-through">
                    {r.name}
                  </span>
                ) : (
                  <Link href={hrefFor(r)} className="font-mono text-sm hover:text-primary">
                    {r.name}
                  </Link>
                )}
              </div>
            ))}
          </Card>
        </>
      )}
    </>
  );
}

function hrefFor(r: TemplateInstall["resources"][number]) {
  switch (r.kind) {
    case "project":
    case "environment":
    case "app":
      return `/projects/${r.name}`;
    case "database":
      return `/databases/${r.name}`;
    case "store":
      return `/storage/${r.name}`;
  }
}
