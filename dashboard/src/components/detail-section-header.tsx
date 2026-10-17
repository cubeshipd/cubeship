import { ArrowLeftIcon } from "lucide-react";
import type { ReactNode } from "react";
import Link from "@/components/navigation-link";
import { SectionHeader } from "@/components/section-header";
import { Button } from "@/components/ui/button";

export function DetailSectionHeader({
  backHref,
  backLabel,
  title,
  sub,
  actions,
}: {
  backHref: string;
  backLabel: string;
  title: ReactNode;
  sub?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <SectionHeader
      page
      title={
        <span className="flex min-w-0 items-center gap-2">
          <Button
            variant="ghost"
            size="icon-sm"
            nativeButton={false}
            render={
              <Link href={backHref} aria-label={backLabel}>
                <ArrowLeftIcon />
              </Link>
            }
          />
          <span className="truncate">{title}</span>
        </span>
      }
      sub={sub}
      actions={actions}
    />
  );
}
