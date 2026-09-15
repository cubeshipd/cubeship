"use client";

import { cn } from "cn";
import { useState } from "react";
import { personName } from "@/lib/api";

type Person = { username: string; display_name?: string; avatar_url?: string };

export function UserAvatar({
  person,
  className,
  src,
}: {
  person: Person;
  className?: string;
  src?: string;
}) {
  const image = src ?? person.avatar_url;
  const [failed, setFailed] = useState<string>();
  const name = personName(person);
  const words = name.trim().split(/\s+/);
  const initials = (
    words.length > 1 ? words[0][0] + words[words.length - 1][0] : name.slice(0, 2)
  ).toUpperCase();

  return (
    <span aria-hidden="true" className={cn("user-avatar", className)}>
      {image && image !== failed ? (
        // biome-ignore lint/performance/noImgElement: authenticated same-origin image; no optimization proxy
        <img
          src={image}
          alt=""
          className="size-full object-cover"
          onError={() => setFailed(image)}
        />
      ) : (
        <span>{initials || "?"}</span>
      )}
    </span>
  );
}
