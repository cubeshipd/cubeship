"use client";

import type { CSSProperties, ReactNode } from "react";
import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";

export type HoldButtonSize = "sm" | "md" | "lg";
export type HoldButtonDirection = "right" | "up";

export interface HoldButtonProps {
  children?: ReactNode;
  doneLabel?: ReactNode;
  icon?: ReactNode;
  doneIcon?: ReactNode;
  backgroundColor?: string;
  fillColor?: string;
  textColor?: string;
  fillTextColor?: string;
  size?: HoldButtonSize;
  radius?: number;
  fillDirection?: HoldButtonDirection;
  holdTime?: number;
  releaseTime?: number;
  pressScale?: number;
  wave?: boolean;
  waveAmplitude?: number;
  glow?: boolean;
  resetAfter?: number;
  disabled?: boolean;
  onHold?: () => void;
  onTap?: () => void;
  className?: string;
}

type Phase = "idle" | "holding" | "done";
type Input = "pointer" | "key" | null;

type Motion = { raf: number; progress: number; from: number; to: number; start: number };
type Gesture = { pointerId: number | null; start: number; rect: DOMRect | null };

const TAP_MS = 250;
const HIT_PAD = 10;
const SIZES: Record<HoldButtonSize, string> = {
  sm: "h-9 px-4 text-[13px]",
  md: "h-11 px-[22px] text-[15px]",
  lg: "h-[52px] px-7 text-[17px]",
};

const STYLE = `
.hb-fill{clip-path:inset(0 calc((1 - var(--hb-p)) * (100% + .75 * var(--hb-wave)) - var(--hb-p) * .25 * var(--hb-wave)) 0 0)}
.hb-root[data-direction=up] .hb-fill{clip-path:inset(calc((1 - var(--hb-p)) * (100% + .75 * var(--hb-wave)) - var(--hb-p) * .25 * var(--hb-wave)) 0 0 0)}
.hb-crest{-webkit-mask-image:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='20' height='200' viewBox='0 0 20 200' preserveAspectRatio='none'%3E%3Cpath d='M0 0H10C18 8 18 25.3 10 33.3S2 58.7 10 66.7S18 92 10 100S2 125.3 10 133.3S18 158.7 10 166.7S2 192 10 200H0Z'/%3E%3C/svg%3E");mask-image:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='20' height='200' viewBox='0 0 20 200' preserveAspectRatio='none'%3E%3Cpath d='M0 0H10C18 8 18 25.3 10 33.3S2 58.7 10 66.7S18 92 10 100S2 125.3 10 133.3S18 158.7 10 166.7S2 192 10 200H0Z'/%3E%3C/svg%3E");mask-repeat:repeat-y;mask-size:var(--hb-wave) calc(var(--hb-h) * 2);mask-position-x:calc(-1 * var(--hb-wave) + var(--hb-p) * (var(--hb-w) + var(--hb-wave)));mask-position-y:calc(-1 * var(--hb-p) * var(--hb-cycles) * var(--hb-h))}
.hb-root[data-direction=up] .hb-crest{-webkit-mask-image:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='200' height='20' viewBox='0 0 200 20' preserveAspectRatio='none'%3E%3Cpath d='M0 20V10C12 2 38 2 50 10S88 18 100 10S138 2 150 10S188 18 200 10V20Z'/%3E%3C/svg%3E");mask-image:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='200' height='20' viewBox='0 0 200 20' preserveAspectRatio='none'%3E%3Cpath d='M0 20V10C12 2 38 2 50 10S88 18 100 10S138 2 150 10S188 18 200 10V20Z'/%3E%3C/svg%3E");mask-repeat:repeat-x;mask-size:calc(var(--hb-w) * 2) var(--hb-wave);mask-position-x:calc(-1 * var(--hb-p) * var(--hb-cycles) * var(--hb-w));mask-position-y:calc(var(--hb-h) - var(--hb-p) * (var(--hb-h) + var(--hb-wave)))}
.hb-crest{opacity:var(--hb-p)}
.hb-root[data-phase=done] .hb-crest{opacity:0}
.hb-root[data-glow=true][data-phase=holding]{box-shadow:inset 0 1px 0 rgba(255,255,255,.06),0 10px 32px -6px color-mix(in srgb,var(--hb-fill) calc(var(--hb-p) * 70%),transparent)}
@keyframes hb-pulse{from{opacity:1;box-shadow:0 0 0 0 color-mix(in srgb,var(--hb-fill) 55%,transparent)}to{opacity:0;box-shadow:0 0 0 14px color-mix(in srgb,var(--hb-fill) 0%,transparent)}}
@media(prefers-reduced-motion:reduce){.hb-root{transform:none!important;transition:background-color 160ms ease,box-shadow var(--hb-release) ease!important}.hb-fill{clip-path:inset(0)!important;opacity:0;transition:opacity var(--hb-release) ease!important}.hb-crest{display:none}.hb-root[data-phase=holding] .hb-fill,.hb-root[data-phase=done] .hb-fill{opacity:1;transition:opacity var(--hb-hold) linear!important}.hb-pulse{animation:none!important}.hb-label>span{filter:none!important;transition:opacity 200ms ease!important}}
`;

const LABEL =
  "[grid-area:1/1] inline-flex items-center gap-2 whitespace-nowrap [transition:opacity_200ms_ease,filter_200ms_ease]";

export default function HoldButton({
  children = "Hold to delete",
  doneLabel = "Deleted",
  icon = null,
  doneIcon = null,
  backgroundColor = "#27272a",
  fillColor = "#5227FF",
  textColor = "#f5f5f5",
  fillTextColor = "#ffffff",
  size = "md",
  radius = 14,
  fillDirection = "right",
  holdTime = 2000,
  releaseTime = 200,
  pressScale = 0.97,
  wave = true,
  waveAmplitude = 6,
  glow = true,
  resetAfter = 1200,
  disabled = false,
  onHold,
  onTap,
  className = "",
}: HoldButtonProps) {
  const [phase, setPhase] = useState<Phase>("idle");
  const [input, setInput] = useState<Input>(null);
  const phaseRef = useRef<Phase>("idle");
  const inputRef = useRef<Input>(null);
  const button = useRef<HTMLButtonElement>(null);
  const gesture = useRef<Gesture>({ pointerId: null, start: 0, rect: null });
  const timers = useRef({ complete: 0, reset: 0 });
  const motion = useRef<Motion>({ raf: 0, progress: 0, from: 0, to: 0, start: 0 });
  const hintId = useId();

  const changePhase = (next: Phase, kind: Input = null) => {
    phaseRef.current = next;
    inputRef.current = kind;
    setPhase(next);
    setInput(kind);
  };

  const clearTimers = () => {
    window.clearTimeout(timers.current.complete);
    window.clearTimeout(timers.current.reset);
  };

  const complete = () => {
    if (phaseRef.current !== "holding" || performance.now() - gesture.current.start < holdTime - 50)
      return;
    clearTimers();
    changePhase("done", inputRef.current);
    onHold?.();
    if (resetAfter > 0) {
      timers.current.reset = window.setTimeout(() => {
        changePhase("idle");
        drive(0, releaseTime, (t) => 1 - (1 - t) ** 3);
      }, resetAfter);
    }
  };

  const drive = (target: number, duration: number, ease: (t: number) => number) => {
    const current = motion.current;
    cancelAnimationFrame(current.raf);
    current.from = current.progress;
    current.to = target;
    current.start = performance.now();
    const step = (now: number) => {
      const t = duration > 0 ? Math.min(1, (now - current.start) / duration) : 1;
      current.progress = current.from + (current.to - current.from) * ease(t);
      button.current?.style.setProperty("--hb-p", current.progress.toFixed(4));
      if (t < 1) current.raf = requestAnimationFrame(step);
      else {
        current.raf = 0;
        if (current.to === 1) complete();
      }
    };
    current.raf = requestAnimationFrame(step);
  };

  const begin = (kind: Input) => {
    if (disabled || phaseRef.current !== "idle" || !button.current) return false;
    gesture.current.start = performance.now();
    gesture.current.rect = button.current.getBoundingClientRect();
    changePhase("holding", kind);
    drive(1, holdTime, (t) => t);
    timers.current.complete = window.setTimeout(complete, holdTime + 100);
    return true;
  };

  const release = (drifted = false) => {
    if (phaseRef.current !== "holding") return;
    clearTimers();
    const held = performance.now() - gesture.current.start;
    changePhase("idle");
    drive(0, releaseTime, (t) => 1 - (1 - t) ** 3);
    if (!drifted && held < TAP_MS) onTap?.();
  };
  const releaseRef = useRef(release);
  releaseRef.current = release;

  useLayoutEffect(() => {
    const element = button.current;
    if (!element) return;
    const measure = () => {
      element.style.setProperty("--hb-w", `${element.offsetWidth}px`);
      element.style.setProperty("--hb-h", `${element.offsetHeight}px`);
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (phase !== "holding") return;
    const cancel = () => releaseRef.current(true);
    const visibility = () => {
      if (document.hidden) cancel();
    };
    window.addEventListener("blur", cancel);
    document.addEventListener("visibilitychange", visibility);
    return () => {
      window.removeEventListener("blur", cancel);
      document.removeEventListener("visibilitychange", visibility);
    };
  }, [phase]);

  useEffect(
    () => () => {
      window.clearTimeout(timers.current.complete);
      window.clearTimeout(timers.current.reset);
      cancelAnimationFrame(motion.current.raf);
    },
    [],
  );

  const labels = (done: boolean) => (
    <>
      <span className={`${LABEL} ${done ? "opacity-0 blur-[2px]" : ""}`} aria-hidden={done}>
        {icon && <span className="inline-flex flex-none [&>svg]:block">{icon}</span>}
        {children}
      </span>
      <span className={`${LABEL} ${done ? "" : "opacity-0 blur-[2px]"}`} aria-hidden={!done}>
        {doneIcon && <span className="inline-flex flex-none [&>svg]:block">{doneIcon}</span>}
        {doneLabel}
      </span>
    </>
  );

  const style = {
    "--hb-radius": `${radius}px`,
    "--hb-bg": backgroundColor,
    "--hb-fill": fillColor,
    "--hb-text": textColor,
    "--hb-fill-text": fillTextColor,
    "--hb-hold": `${holdTime}ms`,
    "--hb-cycles": holdTime / 1100,
    "--hb-release": `${releaseTime}ms`,
    "--hb-press": pressScale,
    "--hb-wave": `${wave ? waveAmplitude : 0}px`,
    "--hb-p": 0,
  } as CSSProperties;

  return (
    <button
      ref={button}
      type="button"
      disabled={disabled}
      className={`hb-root group relative isolate m-0 inline-grid cursor-pointer touch-manipulation select-none place-items-center border-0 font-medium leading-none tracking-[0.01em] outline-none [-webkit-tap-highlight-color:transparent] [background:var(--hb-bg)] [border-radius:var(--hb-radius)] [color:var(--hb-text)] shadow-[inset_0_1px_0_rgba(255,255,255,0.06)] [transition:transform_160ms_ease,background-color_160ms_ease,box-shadow_200ms_ease] [@media(hover:hover)_and_(pointer:fine)]:enabled:hover:[background:color-mix(in_srgb,var(--hb-bg)_92%,#fff)] data-[phase=holding]:data-[input=pointer]:[transform:scale(var(--hb-press))] focus-visible:[outline:2px_solid_var(--hb-fill)] focus-visible:outline-offset-[3px] disabled:pointer-events-none disabled:cursor-default disabled:opacity-50 contrast-more:[outline:1px_solid_var(--hb-text)] ${SIZES[size] || SIZES.md} ${className}`}
      data-phase={phase}
      data-input={input ?? undefined}
      data-direction={fillDirection === "up" ? "up" : "right"}
      data-glow={glow ? "true" : undefined}
      aria-describedby={hintId}
      style={style}
      onPointerDown={(event) => {
        if (event.button !== 0 || !event.isPrimary || gesture.current.pointerId !== null) return;
        if (!begin("pointer")) return;
        gesture.current.pointerId = event.pointerId;
        try {
          event.currentTarget.setPointerCapture(event.pointerId);
        } catch {}
      }}
      onPointerMove={(event) => {
        if (event.pointerId !== gesture.current.pointerId) return;
        const rect = gesture.current.rect;
        if (
          rect &&
          (event.clientX < rect.left - HIT_PAD ||
            event.clientX > rect.right + HIT_PAD ||
            event.clientY < rect.top - HIT_PAD ||
            event.clientY > rect.bottom + HIT_PAD)
        ) {
          gesture.current.pointerId = null;
          release(true);
        }
      }}
      onPointerUp={(event) => {
        if (event.pointerId !== gesture.current.pointerId) return;
        gesture.current.pointerId = null;
        release();
      }}
      onPointerCancel={(event) => {
        if (event.pointerId !== gesture.current.pointerId) return;
        gesture.current.pointerId = null;
        release(true);
      }}
      onLostPointerCapture={(event) => {
        if (event.pointerId !== gesture.current.pointerId) return;
        gesture.current.pointerId = null;
        release(true);
      }}
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          if (inputRef.current === "key") release(true);
          return;
        }
        if (event.key === " " || event.key === "Enter") {
          event.preventDefault();
          if (!event.repeat) begin("key");
        }
      }}
      onKeyUp={(event) => {
        if ((event.key === " " || event.key === "Enter") && inputRef.current === "key") {
          event.preventDefault();
          release();
        }
      }}
      onContextMenu={(event) => event.preventDefault()}
    >
      <style>{STYLE}</style>
      <span
        className="hb-pulse pointer-events-none absolute inset-0 z-0 opacity-0 [border-radius:var(--hb-radius)] group-data-[glow=true]:group-data-[phase=done]:[animation:hb-pulse_600ms_ease-out_forwards]"
        aria-hidden="true"
      />
      <span className="relative z-[2] grid place-items-center">
        <span className="grid place-items-center">{labels(phase === "done")}</span>
      </span>
      <span
        className="pointer-events-none absolute inset-0 z-[3] [clip-path:inset(0_round_var(--hb-radius))]"
        aria-hidden="true"
      >
        <span className="hb-fill absolute inset-0 grid place-items-center [background:var(--hb-fill)] [color:var(--hb-fill-text)]">
          <span className="grid place-items-center">{labels(phase === "done")}</span>
        </span>
        <span className="hb-crest absolute inset-0 grid place-items-center [background:var(--hb-fill)] [color:var(--hb-fill-text)]">
          <span className="grid place-items-center">{labels(phase === "done")}</span>
        </span>
      </span>
      <span id={hintId} className="sr-only">
        Press and hold for {Math.round(holdTime / 100) / 10} seconds to confirm
      </span>
    </button>
  );
}
