"use client";

import { CheckIcon, ChevronsUpDownIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";
import { ActionButton } from "@/components/action-button";
import { CubeMark } from "@/components/brand";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorAlert } from "@/components/error-alert";
import { useOrg } from "@/components/org-context";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { api } from "@/lib/api";
import { message } from "@/lib/errors";
import { sanitize, toSlug } from "@/lib/slug";

// The organization is not a page — it is the frame everything else on
// the screen is inside, so it lives at the top of the sidebar and is
// switched, created and deleted from here.
export function OrgSwitcher() {
  const { orgs, org, current, select, reload } = useOrg();
  const [creating, setCreating] = useState(false);
  const [deleting, setDeleting] = useState(false);

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              className="flex w-full items-center gap-2.5 px-4 py-3.5 text-left transition-colors hover:bg-secondary"
            >
              <CubeMark className="size-5 shrink-0 drop-shadow-[0_0_10px_var(--primary)]" />
              <span className="min-w-0 flex-1">
                <span className="block text-[9px] tracking-[0.26em] text-subtle-foreground uppercase">
                  cubeship
                </span>
                <span className="block truncate font-mono text-xs text-foreground">
                  {org || "no organization"}
                </span>
              </span>
              <ChevronsUpDownIcon className="size-3.5 shrink-0 text-subtle-foreground" />
            </button>
          }
        />

        <DropdownMenuContent align="start" className="w-56">
          <DropdownMenuGroup>
            <DropdownMenuLabel>Organizations</DropdownMenuLabel>
            {orgs.map((o) => (
              <DropdownMenuItem key={o.slug} onClick={() => select(o.slug)}>
                <CheckIcon className={o.slug === org ? "text-primary" : "invisible"} />
                <span className="truncate font-mono text-xs">{o.slug}</span>
              </DropdownMenuItem>
            ))}
            {orgs.length === 0 && (
              <DropdownMenuItem disabled>
                <span className="text-xs">None yet</span>
              </DropdownMenuItem>
            )}
          </DropdownMenuGroup>

          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => setCreating(true)}>
            <PlusIcon />
            New organization
          </DropdownMenuItem>
          {current && (
            <DropdownMenuItem
              onClick={() => setDeleting(true)}
              className="text-destructive data-highlighted:text-destructive"
            >
              <Trash2Icon />
              Delete {current.slug}
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      <CreateDialog
        open={creating}
        onOpenChange={setCreating}
        onCreated={async (slug) => {
          await reload();
          select(slug);
        }}
      />
      {current && (
        <ConfirmDialog
          open={deleting}
          onOpenChange={setDeleting}
          title="Delete organization"
          description="It has to hold no projects first — nothing cascades into stopping containers behind your back."
          confirmWord={current.slug}
          confirmLabel="Delete organization"
          onConfirm={async () => {
            await api.del(`/orgs/${current.slug}`);
            await reload();
          }}
        />
      )}
    </>
  );
}

function CreateDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onCreated: (slug: string) => Promise<void>;
}) {
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  // The slug follows the name until someone edits it, and then stops:
  // a slug you corrected must not be overwritten by the next keystroke
  // in the field above it.
  const [slugEdited, setSlugEdited] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post("/orgs", { slug, name: name || slug });
      await onCreated(slug);
      setName("");
      setSlug("");
      setSlugEdited(false);
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>New organization</DialogTitle>
            <DialogDescription>
              The slug is derived from the name, and becomes the first segment of every app
              reference and registry path under it — so it cannot be changed later.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-5">
            <ErrorAlert error={error} className="mb-0" />
            <TextField
              label="Name"
              autoFocus
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                if (!slugEdited) setSlug(toSlug(e.target.value));
              }}
              placeholder="Acme Industries"
            />
            <TextField
              label="Slug"
              hint="Lowercase letters, digits and dashes. Edit it and it stops following the name."
              className="font-mono"
              spellCheck={false}
              value={slug}
              onChange={(e) => {
                setSlugEdited(true);
                setSlug(sanitize(e.target.value));
              }}
              placeholder="acme-industries"
            />
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!slug}>
              Create
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
