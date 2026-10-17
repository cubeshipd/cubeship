"use client";

import { ArrowDown01Icon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { GLIDE_FIELD_TRIGGER_CLASS, GlideSelect } from "@/components/glide-select";
import type { Choice } from "@/components/searchable-select";

const SEARCH_FROM = 8;

export function MultiSelect({
  label,
  ariaLabel,
  hint,
  none,
  empty = "Nothing to choose from.",
  choices,
  values,
  onChange,
  disabled,
}: {
  label?: string;
  ariaLabel?: string;
  hint?: string;
  none: string;
  empty?: string;
  choices: Choice[];
  values: string[];
  onChange: (values: string[]) => void;
  disabled?: boolean;
}) {
  const chosen = choices.filter((choice) => values.includes(choice.value));

  return (
    <div>
      <GlideSelect
        label={label}
        ariaLabel={ariaLabel ?? label ?? none}
        className="w-full"
        field
        triggerClassName={GLIDE_FIELD_TRIGGER_CLASS}
        triggerContent={
          <>
            <span
              className={
                chosen.length === 0
                  ? "min-w-0 truncate text-muted-foreground"
                  : "min-w-0 truncate font-mono text-xs"
              }
            >
              {chosen.length === 0 ? none : chosen.map((choice) => choice.label).join(", ")}
            </span>
            <span className="flex shrink-0 items-center gap-2">
              {chosen.length > 1 && (
                <span className="font-mono text-[11px] text-muted-foreground">{chosen.length}</span>
              )}
              <HugeiconsIcon icon={ArrowDown01Icon} size={14} className="text-muted-foreground" />
            </span>
          </>
        }
        options={[
          { value: "", label: none },
          ...choices.map((choice) => ({
            value: choice.value,
            label: choice.label,
            icon: choice.icon ? <choice.icon className="size-4" /> : undefined,
            tag: choice.hint,
          })),
        ]}
        value={values[0] ?? ""}
        selectedValues={values.length === 0 ? [""] : values}
        onChange={(value) =>
          onChange(
            value === ""
              ? []
              : values.includes(value)
                ? values.filter((item) => item !== value)
                : [...values, value],
          )
        }
        closeOnSelect={false}
        searchable={choices.length >= SEARCH_FROM}
        emptyMessage={empty}
        disabled={disabled}
      />
      {hint && <p className="mt-1 text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}
