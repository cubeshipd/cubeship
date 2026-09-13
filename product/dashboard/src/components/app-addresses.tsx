"use client";

import { useQuery } from "@tanstack/react-query";
import { ExternalLinkIcon } from "lucide-react";
import { CopyButton } from "@/components/copy-button";
import { RowAction } from "@/components/row-actions";
import { SectionHeader } from "@/components/section-header";
import { Card } from "@/components/ui/card";
import { type App, api, type Settings } from "@/lib/api";
import { internalUrl, publicUrl } from "@/lib/app-urls";

// Every address an app answers at, to copy without opening its settings:
// the internal one always, and each public name it has. Changing them is
// the settings' Network tab.
export function AppAddresses({ app }: { app: App }) {
  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.get<Settings>("/settings"),
  });
  const internal = internalUrl(app);

  return (
    <>
      <SectionHeader title="Networking" />
      <Card className="gap-0 divide-y divide-border py-0">
        <Address kind="Internal" url={internal} />
        {app.domains.map((d) => (
          <Address
            key={d.id}
            kind="Public"
            url={publicUrl(d.host, settings.data?.tls_enabled)}
            external
          />
        ))}
      </Card>
    </>
  );
}

function Address({
  kind,
  url,
  external = false,
}: {
  kind: string;
  url: string;
  external?: boolean;
}) {
  return (
    <div className="flex h-11 items-center gap-4 px-4">
      <span className="w-16 shrink-0 text-[0.6875rem] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
        {kind}
      </span>
      <span className="min-w-0 flex-1 truncate font-mono text-xs" title={url}>
        {url}
      </span>
      <span className="flex shrink-0 items-center gap-1">
        <CopyButton value={url} label={`Copy ${url}`} />
        {external && <RowAction icon={ExternalLinkIcon} label={`Open ${url}`} href={url} />}
      </span>
    </div>
  );
}
