"use client";

import { ImagePlusIcon, Trash2Icon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { useSession, useUpdateSession } from "@/components/session-context";
import { Button } from "@/components/ui/button";
import { UserAvatar } from "@/components/user-avatar";
import { api, type Me } from "@/lib/api";
import { message } from "@/lib/errors";

// The browser handles decoding and cropping; the daemon receives a small icon.
async function prepare(file: File): Promise<Blob> {
  if (!["image/png", "image/jpeg", "image/webp"].includes(file.type)) {
    throw new Error("Choose a PNG, JPEG or WebP image.");
  }
  if (file.size > 10 * 1024 * 1024) throw new Error("Choose an image smaller than 10 MB.");
  const bitmap = await createImageBitmap(file);
  try {
    const side = Math.min(bitmap.width, bitmap.height);
    const canvas = document.createElement("canvas");
    canvas.width = canvas.height = 256;
    const context = canvas.getContext("2d");
    if (!context) throw new Error("This browser could not prepare the image.");
    context.drawImage(
      bitmap,
      (bitmap.width - side) / 2,
      (bitmap.height - side) / 2,
      side,
      side,
      0,
      0,
      256,
      256,
    );
    return await new Promise<Blob>((resolve, reject) =>
      canvas.toBlob(
        (blob) => (blob ? resolve(blob) : reject(new Error("The image could not be prepared."))),
        "image/png",
      ),
    );
  } finally {
    bitmap.close();
  }
}

export function ProfileImage() {
  const me = useSession();
  const updateSession = useUpdateSession();
  const input = useRef<HTMLInputElement>(null);
  const [chosen, setChosen] = useState<Blob | null>(null);
  const [preview, setPreview] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [status, setStatus] = useState("");

  useEffect(() => {
    if (!chosen) {
      setPreview(undefined);
      return;
    }
    const url = URL.createObjectURL(chosen);
    setPreview(url);
    return () => URL.revokeObjectURL(url);
  }, [chosen]);

  async function choose(file?: File) {
    if (!file) return;
    setBusy(true);
    setError(null);
    setStatus("");
    try {
      setChosen(await prepare(file));
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  async function save(remove = false) {
    if (!remove && !chosen) return;
    setBusy(true);
    setError(null);
    setStatus("");
    try {
      if (remove) await api.del("/users/me/avatar");
      else if (chosen) await api.putBytes("/users/me/avatar", chosen);
      const updated = await api.get<Me>("/users/me");
      updateSession({ avatar: updated.avatar, avatar_url: updated.avatar_url });
      setChosen(null);
      setStatus(remove ? "Profile image removed." : "Profile image saved.");
    } catch (err) {
      setError(message(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section aria-label="Profile image" className="profile-image" aria-busy={busy}>
      <div className="profile-image-preview">
        <UserAvatar person={me} src={preview} className="size-20" />
      </div>
      <div className="min-w-0 flex-1 space-y-3">
        <div>
          <h3 className="text-sm font-medium">Profile image</h3>
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
            PNG, JPEG or WebP, up to 10 MB. Your image is cropped to a square.
          </p>
        </div>
        <input
          ref={input}
          type="file"
          accept="image/png,image/jpeg,image/webp"
          aria-label="Choose profile image"
          className="hidden"
          disabled={busy}
          onChange={(event) => {
            const file = event.currentTarget.files?.[0];
            event.currentTarget.value = "";
            void choose(file);
          }}
        />
        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => input.current?.click()}
          >
            <ImagePlusIcon />
            {me.avatar_url || chosen ? "Change image" : "Upload image"}
          </Button>
          {chosen && (
            <>
              <ActionButton type="button" size="sm" busy={busy} onClick={() => void save()}>
                Save image
              </ActionButton>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                disabled={busy}
                onClick={() => {
                  setChosen(null);
                  setError(null);
                }}
              >
                Cancel
              </Button>
            </>
          )}
          {me.avatar_url && !chosen && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={busy}
              onClick={() => void save(true)}
            >
              <Trash2Icon />
              Remove
            </Button>
          )}
        </div>
        <ErrorAlert error={error} />
        <p role="status" className="text-xs text-success">
          {busy
            ? "Updating image…"
            : status || (chosen ? "Preview ready. Save to apply your new image." : "")}
        </p>
      </div>
    </section>
  );
}
