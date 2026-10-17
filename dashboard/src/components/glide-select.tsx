"use client";

import { ArrowDown01Icon, Tick02Icon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import {
  type ReactNode,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { SearchBar } from "@/components/search-bar";
import { Label } from "@/components/ui/label";

export type GlideSelectOption = {
  value: string;
  label: ReactNode;
  icon?: ReactNode;
  tag?: string;
  searchText?: string;
};
type Phase = "closed" | "open";

export function GlideSelect({
  options,
  label,
  value,
  defaultValue,
  onChange,
  placeholder = "Select…",
  ariaLabel = "Select",
  startIcon,
  triggerContent,
  triggerClassName,
  accent = false,
  align = "left",
  mobileAlign,
  searchable = false,
  disabled = false,
  busy = false,
  emptyMessage = "No options found.",
  className = "",
}: {
  options: GlideSelectOption[];
  label?: string;
  value?: string;
  defaultValue?: string;
  onChange: (value: string, option: GlideSelectOption) => void;
  placeholder?: string;
  ariaLabel?: string;
  startIcon?: ReactNode;
  triggerContent?: ReactNode;
  triggerClassName?: string;
  accent?: boolean;
  align?: "left" | "right";
  mobileAlign?: "left" | "right";
  searchable?: boolean;
  disabled?: boolean;
  busy?: boolean;
  emptyMessage?: string;
  className?: string;
}) {
  const [inner, setInner] = useState(defaultValue ?? "");
  const selected = value ?? inner;
  const selectedOption = options.find((option) => option.value === selected);
  const [active, setActive] = useState<number | null>(null);
  const [phase, setPhase] = useState<Phase>("closed");
  const [mounted, setMounted] = useState(false);
  const [query, setQuery] = useState("");
  const [side, setSide] = useState<"top" | "bottom">("bottom");
  const [resolvedAlign, setResolvedAlign] = useState(align);
  const [position, setPosition] = useState<{ top: number; left?: number; right?: number }>({
    top: 0,
    left: 0,
  });
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const pill = useRef<HTMLSpanElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const unmountTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const id = useId();
  const listId = `${id}-list`;
  const triggerId = `${id}-trigger`;
  const visibleOptions = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return options
      .map((option, index) => ({ option, index }))
      .filter(({ option }) => {
        if (!searchable || !needle) return true;
        const text = option.searchText ?? (typeof option.label === "string" ? option.label : "");
        return `${text} ${option.value}`.toLowerCase().includes(needle);
      });
  }, [options, query, searchable]);

  useEffect(() => {
    if (phase !== "open") return;
    if (active === null || !visibleOptions.some(({ index }) => index === active)) {
      const selectedIndex = visibleOptions.find(({ option }) => option.value === selected)?.index;
      setActive(selectedIndex ?? visibleOptions[0]?.index ?? null);
    }
  }, [active, phase, selected, visibleOptions]);

  useLayoutEffect(() => {
    if (phase !== "open" || !root.current || !menu.current) return;
    const place = () => {
      const rect = trigger.current?.getBoundingClientRect();
      const el = menu.current;
      if (!rect || !el) return;
      const nextAlign =
        mobileAlign && window.matchMedia("(max-width: 760px)").matches ? mobileAlign : align;
      setResolvedAlign(nextAlign);
      el.style.width = `${Math.min(window.innerWidth - 16, Math.max(el.offsetWidth, rect.width))}px`;
      const nextSide = rect.bottom + el.offsetHeight + 6 > window.innerHeight ? "top" : "bottom";
      setSide(nextSide);
      const top = nextSide === "bottom" ? rect.bottom + 6 : rect.top - el.offsetHeight - 6;
      const left = nextAlign === "right" ? rect.right - el.offsetWidth : rect.left;
      setPosition({
        top,
        left: Math.max(8, Math.min(left, window.innerWidth - el.offsetWidth - 8)),
      });
      el.dataset.state = "closed";
      void el.offsetHeight;
      el.dataset.state = "open";
    };
    place();
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [phase, align, mobileAlign]);

  useLayoutEffect(() => {
    if (phase !== "open" || !pill.current || active === null) return;
    const items = pill.current.parentElement?.querySelectorAll<HTMLElement>('[role="option"]');
    const optionIndex = visibleOptions.findIndex(({ index }) => index === active);
    const option = optionIndex < 0 ? undefined : items?.[optionIndex];
    if (!option) return;
    pill.current.style.height = `${option.offsetHeight}px`;
    pill.current.style.transform = `translateY(${option.offsetTop}px)`;
  }, [active, phase, visibleOptions]);

  const close = useCallback(() => {
    setPhase("closed");
    if (unmountTimer.current) clearTimeout(unmountTimer.current);
    unmountTimer.current = setTimeout(() => {
      setMounted(false);
      unmountTimer.current = null;
    }, 150);
  }, []);

  useEffect(() => {
    if (phase === "closed") return;
    const closeOutside = (event: PointerEvent) => {
      const target = event.target as Node;
      if (!root.current?.contains(target) && !menu.current?.contains(target)) close();
    };
    document.addEventListener("pointerdown", closeOutside, true);
    return () => document.removeEventListener("pointerdown", closeOutside, true);
  }, [phase, close]);

  useEffect(
    () => () => {
      if (unmountTimer.current) clearTimeout(unmountTimer.current);
    },
    [],
  );

  function pick(index: number) {
    const option = options[index];
    if (!option) return;
    if (value === undefined) setInner(option.value);
    onChange(option.value, option);
    close();
  }

  function open() {
    if (disabled || busy) return;
    if (unmountTimer.current) clearTimeout(unmountTimer.current);
    unmountTimer.current = null;
    setMounted(true);
    setQuery("");
    setActive(
      Math.max(
        0,
        options.findIndex((option) => option.value === selected),
      ),
    );
    setPhase("open");
    if (searchable) requestAnimationFrame(() => search.current?.focus());
  }

  function onKeyDown(event: React.KeyboardEvent<HTMLButtonElement>) {
    const current = active ?? visibleOptions[0]?.index ?? 0;
    const currentPosition = visibleOptions.findIndex(({ index }) => index === current);
    if (phase !== "open") {
      if (["Enter", " ", "ArrowDown", "ArrowUp"].includes(event.key)) {
        event.preventDefault();
        open();
      }
      return;
    }
    if (event.key === "Escape" || event.key === "Tab") {
      if (event.key === "Escape") event.preventDefault();
      close();
    } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      setActive(
        visibleOptions[
          Math.max(
            0,
            Math.min(
              visibleOptions.length - 1,
              (currentPosition < 0 ? 0 : currentPosition) + (event.key === "ArrowDown" ? 1 : -1),
            ),
          )
        ]?.index ?? null,
      );
    } else if (event.key === "Home" || event.key === "End") {
      event.preventDefault();
      setActive((event.key === "Home" ? visibleOptions[0] : visibleOptions.at(-1))?.index ?? null);
    } else if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      pick(current);
    }
  }

  function onSearchKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Tab") {
      close();
    } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const position = visibleOptions.findIndex(({ index }) => index === active);
      const next = Math.max(
        0,
        Math.min(
          visibleOptions.length - 1,
          (position < 0 ? -1 : position) + (event.key === "ArrowDown" ? 1 : -1),
        ),
      );
      setActive(visibleOptions[next]?.index ?? null);
    } else if (event.key === "Enter") {
      event.preventDefault();
      if (active !== null) pick(active);
    } else if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      close();
    }
  }

  return (
    <div ref={root} className={`relative inline-block ${className}`}>
      {label && (
        <Label htmlFor={triggerId} className="mb-2 block text-xs text-muted-foreground">
          {label}
        </Label>
      )}
      <button
        ref={trigger}
        id={triggerId}
        data-slot={label ? "select-trigger" : undefined}
        type="button"
        role="combobox"
        aria-label={label ? undefined : ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={phase === "open"}
        aria-controls={listId}
        aria-activedescendant={active === null ? undefined : `${id}-${active}`}
        aria-busy={busy || undefined}
        disabled={disabled || busy}
        className={
          triggerClassName ??
          `inline-flex h-9 cursor-pointer items-center gap-2 rounded-lg border px-3 text-sm font-medium shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 ${accent ? "border-primary bg-primary text-primary-foreground hover:bg-primary/85" : "border-border-strong bg-secondary text-foreground hover:border-primary/40 hover:bg-accent"}`
        }
        onClick={(event) => {
          event.stopPropagation();
          phase === "open" ? close() : open();
        }}
        onKeyDown={onKeyDown}
      >
        {triggerContent ?? (
          <>
            <span className="flex min-w-0 items-center gap-2">
              {startIcon && (
                <span
                  aria-hidden="true"
                  className={`inline-flex size-4 shrink-0 items-center justify-center ${accent ? "text-primary-foreground" : "text-primary"}`}
                >
                  {startIcon}
                </span>
              )}
              {selectedOption?.icon && (
                <span
                  aria-hidden="true"
                  className="inline-flex size-4 shrink-0 items-center justify-center text-muted-foreground"
                >
                  {selectedOption.icon}
                </span>
              )}
              <span className="truncate">
                {busy ? "Loading…" : (selectedOption?.label ?? placeholder)}
              </span>
            </span>
            <HugeiconsIcon
              icon={ArrowDown01Icon}
              size={14}
              strokeWidth={2}
              className={accent ? "text-primary-foreground" : "text-muted-foreground"}
            />
          </>
        )}
      </button>
      {mounted &&
        typeof document !== "undefined" &&
        createPortal(
          <div
            ref={menu}
            data-glide-select-content
            className={`fixed z-50 w-48 scale-95 rounded-xl border border-border-strong bg-popover p-1 opacity-0 shadow-xl transition-[transform,opacity] duration-150 data-[state=open]:scale-100 data-[state=open]:opacity-100 data-[state=closed]:pointer-events-none ${resolvedAlign === "right" ? "origin-top-right" : "origin-top-left"}`}
            data-side={side}
            data-align={resolvedAlign}
            data-state={phase}
            style={{
              top: position.top,
              left: position.left,
              right: position.right,
              maxWidth: "calc(100vw - 16px)",
            }}
          >
            {searchable && (
              <SearchBar
                inputRef={search}
                value={query}
                onChange={(next) => {
                  setQuery(next);
                  setActive(null);
                }}
                placeholder="Search"
                onKeyDown={onSearchKeyDown}
                className="mb-1 rounded-none border-0 bg-transparent px-2 focus-within:ring-0"
              />
            )}
            <div
              id={listId}
              role="listbox"
              aria-label={label ?? ariaLabel}
              className="relative grid max-h-64 gap-px overflow-y-auto"
            >
              {visibleOptions.length > 0 && (
                <span
                  ref={pill}
                  aria-hidden="true"
                  className="pointer-events-none absolute inset-x-0 top-0 h-10 rounded-lg bg-accent transition-transform duration-200"
                />
              )}
              {visibleOptions.map(({ option, index }) => (
                <button
                  key={option.value}
                  type="button"
                  role="option"
                  aria-selected={option.value === selected}
                  id={`${id}-${index}`}
                  className="relative z-[1] flex h-10 cursor-pointer items-center gap-2 rounded-lg px-3 text-left text-sm text-foreground hover:bg-transparent focus-visible:outline-none"
                  onPointerEnter={() => setActive(index)}
                  onClick={(event) => {
                    event.stopPropagation();
                    pick(index);
                  }}
                >
                  {option.icon && (
                    <span
                      aria-hidden="true"
                      className="inline-flex size-4 shrink-0 items-center justify-center text-muted-foreground"
                    >
                      {option.icon}
                    </span>
                  )}
                  <span className="min-w-0 flex-1 truncate">{option.label}</span>
                  {option.tag && (
                    <span className="text-xs text-muted-foreground">{option.tag}</span>
                  )}
                  {option.value === selected && (
                    <HugeiconsIcon icon={Tick02Icon} size={14} className="text-primary" />
                  )}
                </button>
              ))}
            </div>
            {visibleOptions.length === 0 && (
              <p className="px-3 py-4 text-center text-xs text-muted-foreground">{emptyMessage}</p>
            )}
          </div>,
          document.body,
        )}
    </div>
  );
}
