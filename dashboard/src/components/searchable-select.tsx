"use client";

import type { ComponentType } from "react";
import { GLIDE_FIELD_TRIGGER_CLASS, GlideSelect } from "@/components/glide-select";

const SEARCH_FROM = 8;

export type Choice = {
  value: string;
  label: string;
  icon?: ComponentType<{ className?: string }>;
  hint?: string;
};

export function SearchableSelect({
  label,
  hint,
  placeholder = "Select…",
  empty = "Nothing to choose from.",
  choices,
  value,
  onChange,
  disabled,
  busy,
  searchable,
  fieldClassName,
  ariaLabel,
}: {
  label?: string;
  hint?: string;
  placeholder?: string;
  empty?: string;
  choices: Choice[];
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  busy?: boolean;
  searchable?: boolean;
  fieldClassName?: string;
  ariaLabel?: string;
}) {
  return (
    <div className={fieldClassName}>
      <GlideSelect
        label={label}
        ariaLabel={ariaLabel ?? label ?? placeholder}
        className="w-full"
        field
        triggerClassName={GLIDE_FIELD_TRIGGER_CLASS}
        placeholder={placeholder}
        emptyMessage={empty}
        options={choices.map((choice) => ({
          value: choice.value,
          label: choice.label,
          icon: choice.icon ? <choice.icon className="size-4" /> : undefined,
          tag: choice.hint,
        }))}
        value={value}
        onChange={onChange}
        disabled={disabled}
        busy={busy}
        searchable={searchable ?? choices.length >= SEARCH_FROM}
      />
      {hint && <p className="mt-1 text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}
