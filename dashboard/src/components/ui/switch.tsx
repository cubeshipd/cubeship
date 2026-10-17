"use client";

import {
  animate,
  motion,
  useMotionValue,
  useReducedMotion,
  useSpring,
  useTransform,
  useVelocity,
} from "motion/react";
import { cn } from "cn";
import {
  useEffect,
  useId,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type CSSProperties,
  type KeyboardEvent,
  type PointerEvent,
} from "react";

type Grip = {
  id: number;
  grab: number | null;
  moved: boolean;
  startX: number;
  startedAt: number;
  onAtPress: boolean;
  slop: number;
};

type SwitchProps = Omit<
  ButtonHTMLAttributes<HTMLButtonElement>,
  "checked" | "defaultChecked" | "onChange"
> & {
  checked?: boolean;
  defaultChecked?: boolean;
  onChange?: (checked: boolean) => void;
  onCheckedChange?: (checked: boolean) => void;
  onTap?: () => void;
  label?: string;
  trackColor?: string;
  trackOnColor?: string;
  thumbColor?: string;
  thumbOnColor?: string;
  width?: number;
  height?: number;
  radius?: number;
  speed?: number;
  stretch?: number;
  hoverScale?: number;
  colorDuration?: number;
  ariaLabel?: string;
};

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));
const FLOW_SPRING = { stiffness: 320, damping: 40, mass: 0.6 };
const SWELL_SPRING = { stiffness: 520, damping: 34, mass: 0.6 };
const TAP_SLOP = { fine: 4, coarse: 8 };

function Switch({
  checked,
  defaultChecked = false,
  onChange,
  onCheckedChange,
  onTap,
  label = "",
  disabled = false,
  trackColor = "var(--input)",
  trackOnColor = "var(--primary)",
  thumbColor = "var(--foreground)",
  thumbOnColor = "var(--primary-foreground)",
  width = 44,
  height = 24,
  radius = 12,
  speed = 50,
  stretch = 36,
  hoverScale = 1.035,
  colorDuration = 320,
  ariaLabel,
  className,
  id,
  style: buttonStyle,
  ...buttonProps
}: SwitchProps) {
  const reduce = useReducedMotion();
  const inset = Math.max(3, Math.round(height * 0.11));
  const thumb = height - inset * 2;
  const min = inset;
  const max = width - inset - thumb;
  const mid = (min + max) / 2;
  const trackRadius = Math.min(radius, height / 2);
  const thumbRadius = Math.max(2, trackRadius - inset);

  const controlled = checked !== undefined;
  const [inner, setInner] = useState(defaultChecked);
  const on = controlled ? checked : inner;
  const [dragging, setDragging] = useState(false);
  const trackRef = useRef<HTMLSpanElement>(null);
  const grip = useRef<Grip | null>(null);
  const onRef = useRef(on);
  onRef.current = on;
  const skipClick = useRef(false);
  const autoId = useId();
  const buttonId = id ?? autoId;

  const x = useMotionValue(on ? max : min);
  const flow = useSpring(useVelocity(x), FLOW_SPRING);
  const swell = useSpring(1, SWELL_SPRING);
  const gain = reduce ? 0 : clamp(stretch, 0, 100) / 100;
  const stretchOf = (v: number) => 1 + Math.min(0.4, Math.abs(v) / 600) * gain;
  const scaleX = useTransform([flow, swell], ([v, h]: number[]) => stretchOf(v) * h);
  const scaleY = useTransform([flow, swell], ([v, h]: number[]) => h / stretchOf(v));

  const commit = (next: boolean) => {
    if (next === onRef.current) return;
    onRef.current = next;
    if (!controlled) setInner(next);
    onChange?.(next);
    onCheckedChange?.(next);
  };

  useEffect(() => {
    if (dragging) return undefined;
    const target = on ? max : min;
    if (reduce) {
      x.jump(target);
      return undefined;
    }
    const controls = animate(x, target, {
      type: "spring",
      stiffness: 170 - (50 - clamp(speed, 0, 100)) * 1.1,
      damping: 21.5,
      mass: 0.9,
      restDelta: 0.001,
      restSpeed: 0.01,
    });
    return () => controls.stop();
  }, [on, dragging, min, max, speed, reduce, x]);

  const localX = (clientX: number) => {
    const element = trackRef.current;
    if (!element) return 0;
    const rect = element.getBoundingClientRect();
    const scale = rect.width / (element.offsetWidth || rect.width) || 1;
    return (clientX - rect.left) / scale;
  };

  const down = (event: PointerEvent<HTMLButtonElement>) => {
    if (disabled || grip.current || event.button !== 0) return;
    grip.current = {
      id: event.pointerId,
      grab: null,
      moved: false,
      startX: event.clientX,
      startedAt: performance.now(),
      onAtPress: onRef.current,
      slop: event.pointerType === "touch" ? TAP_SLOP.coarse : TAP_SLOP.fine,
    };
    try {
      event.currentTarget.setPointerCapture(event.pointerId);
    } catch {}
    setDragging(true);
  };

  const move = (event: PointerEvent<HTMLButtonElement>) => {
    const current = grip.current;
    if (!current || current.id !== event.pointerId) return;
    const position = localX(event.clientX);
    if (current.grab === null) {
      current.grab = position - x.get();
      return;
    }
    if (!current.moved && Math.abs(event.clientX - current.startX) > current.slop) {
      current.moved = true;
    }
    if (!current.moved) return;
    const next = clamp(position - current.grab, min, max);
    x.set(next);
    commit(next > mid);
  };

  const up = (
    event: { pointerId: number; currentTarget: HTMLButtonElement },
    cancelled: boolean,
  ) => {
    const current = grip.current;
    if (!current || current.id !== event.pointerId) return;
    grip.current = null;
    try {
      if (event.currentTarget.hasPointerCapture(event.pointerId)) {
        event.currentTarget.releasePointerCapture(event.pointerId);
      }
    } catch {}
    if (cancelled) commit(current.onAtPress);
    else if (!current.moved) {
      commit(!onRef.current);
      if (performance.now() - current.startedAt < 250) onTap?.();
    }
    skipClick.current = true;
    window.setTimeout(() => {
      skipClick.current = false;
    }, 0);
    setDragging(false);
  };

  const click = () => {
    if (skipClick.current) {
      skipClick.current = false;
      return;
    }
    if (!disabled) commit(!onRef.current);
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key === "Escape" && grip.current) {
      up({ pointerId: grip.current.id, currentTarget: event.currentTarget }, true);
    }
  };

  const style = {
    "--ss-w": `${width}px`,
    "--ss-h": `${height}px`,
    "--ss-inset": `${inset}px`,
    "--ss-thumb": `${thumb}px`,
    "--ss-r": `${trackRadius}px`,
    "--ss-thumb-r": `${thumbRadius}px`,
    "--ss-track": trackColor,
    "--ss-track-on": trackOnColor,
    "--ss-thumb-color": thumbColor || `color-mix(in srgb, ${trackOnColor} 19%, ${trackColor})`,
    "--ss-thumb-on": thumbOnColor || trackColor,
    "--ss-fade": `${colorDuration}ms`,
  } as CSSProperties;

  return (
    <span className={cn("inline-flex items-center gap-2.5", className)}>
      <button
        {...buttonProps}
        id={buttonId}
        type="button"
        role="switch"
        disabled={disabled}
        aria-checked={on}
        aria-disabled={disabled || undefined}
        aria-label={ariaLabel ?? buttonProps["aria-label"]}
        className="group relative m-0 inline-block cursor-pointer touch-pan-y border-0 bg-transparent p-0 outline-none select-none after:absolute after:-inset-2 after:content-[''] data-[held]:cursor-grabbing focus-visible:rounded-[var(--ss-r)] focus-visible:outline-2 focus-visible:outline-ring focus-visible:outline-offset-2 disabled:cursor-not-allowed disabled:opacity-50 [-webkit-tap-highlight-color:transparent] [-webkit-touch-callout:none]"
        data-on={on ? "" : undefined}
        data-held={dragging ? "" : undefined}
        style={{ ...style, ...buttonStyle }}
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={(event) => up(event, false)}
        onPointerCancel={(event) => up(event, true)}
        onLostPointerCapture={(event) => up(event, true)}
        onPointerEnter={(event) => {
          if (event.pointerType === "mouse" && !disabled) swell.set(hoverScale);
        }}
        onPointerLeave={() => swell.set(1)}
        onKeyDown={handleKeyDown}
        onClick={click}
      >
        <span
          ref={trackRef}
          className="relative block [width:var(--ss-w)] [height:var(--ss-h)] [border-radius:var(--ss-r)] [background:var(--ss-track)] [transition:background-color_var(--ss-fade)_ease] group-data-[on]:[background:var(--ss-track-on)] motion-reduce:[transition-duration:1ms]"
        >
          <motion.span
            aria-hidden="true"
            className="absolute left-0 [top:var(--ss-inset)] [width:var(--ss-thumb)] [height:var(--ss-thumb)] [border-radius:var(--ss-thumb-r)] [background:var(--ss-thumb-color)] [transition:background-color_var(--ss-fade)_ease] group-data-[on]:[background:var(--ss-thumb-on)] motion-reduce:[transition-duration:1ms]"
            style={{ x, scaleX, scaleY }}
          />
        </span>
      </button>
      {label && (
        <label htmlFor={buttonId} className="cursor-pointer select-none">
          {label}
        </label>
      )}
    </span>
  );
}

export { Switch };
