import { ActionButton } from "@/components/action-button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export function PublishPortForm({
  id,
  port,
  onPortChange,
  onPublish,
  busy,
  hint,
}: {
  id: string;
  port: string;
  onPortChange: (port: string) => void;
  onPublish: () => void | Promise<void>;
  busy: boolean;
  hint: string;
}) {
  return (
    <form
      className="max-w-xl space-y-4"
      onSubmit={(event) => {
        event.preventDefault();
        void onPublish();
      }}
    >
      <div className="space-y-2">
        <Label htmlFor={id}>Host port</Label>
        <Input
          id={id}
          value={port}
          inputMode="numeric"
          spellCheck={false}
          placeholder="Choose automatically"
          onChange={(event) => onPortChange(event.target.value.replace(/\D/g, ""))}
          className="h-10"
        />
        <p className="text-xs text-muted-foreground">{hint}</p>
      </div>
      <ActionButton type="submit" busy={busy}>
        Publish port
      </ActionButton>
    </form>
  );
}
