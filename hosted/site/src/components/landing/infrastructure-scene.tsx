"use client";

import type { ReactNode } from "react";
import { useEffect, useRef, useState } from "react";
import type { SceneRenderer, SceneVariant } from "./scene-renderer";
import "./hero-scene.css";

export function InfrastructureScene({
  fallback,
  variant,
}: {
  fallback: ReactNode;
  variant: SceneVariant;
}) {
  const rootRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const [rendered, setRendered] = useState(false);

  useEffect(() => {
    const root = rootRef.current;
    const canvas = canvasRef.current;
    if (!root || !canvas) return;

    const motionQuery = window.matchMedia("(prefers-reduced-motion: reduce)");
    const pointer = { active: false, x: 0, y: 0 };
    const surface = root.parentElement ?? root;
    let renderer: SceneRenderer | null = null;
    let resizeObserver: ResizeObserver | null = null;
    let intersectionObserver: IntersectionObserver | null = null;
    let animationFrame = 0;
    let generation = 0;
    let initializing = false;
    let visible = false;
    let documentHidden = document.hidden;
    let reducedMotion = motionQuery.matches;
    let contextLost = false;
    let failed = false;
    let mounted = true;
    let hasRendered = false;
    const startedAt = performance.now();

    const canAnimate = () =>
      mounted && visible && !documentHidden && !reducedMotion && !contextLost && !failed;

    const stopAnimation = () => {
      if (animationFrame !== 0) cancelAnimationFrame(animationFrame);
      animationFrame = 0;
    };

    const disposeRenderer = () => {
      generation += 1;
      initializing = false;
      stopAnimation();
      if (!renderer) return;
      try {
        renderer.dispose();
      } catch {
        // The context may already be unavailable. Its DOM listeners are still removed below.
      }
      renderer = null;
    };

    const showFallback = () => {
      hasRendered = false;
      if (mounted) setRendered(false);
    };

    const fail = () => {
      failed = true;
      showFallback();
      disposeRenderer();
    };

    const resize = (width = root.clientWidth, height = root.clientHeight) => {
      if (!renderer || width <= 0 || height <= 0) return;
      try {
        renderer.resize(width, height, window.devicePixelRatio || 1);
      } catch {
        fail();
      }
    };

    const scrollProgress = () => {
      const bounds = root.getBoundingClientRect();
      const travel = Math.max(bounds.height, window.innerHeight * 0.65, 1);
      return Math.min(Math.max(-bounds.top / travel, 0), 1);
    };

    const animate = (now: number) => {
      animationFrame = 0;
      if (!renderer || !canAnimate()) return;

      try {
        renderer.render({
          elapsedSeconds: (now - startedAt) / 1000,
          pointerX: pointer.active ? pointer.x : 0,
          pointerY: pointer.active ? pointer.y : 0,
          scrollProgress: scrollProgress(),
        });
        if (!hasRendered) {
          hasRendered = true;
          setRendered(true);
        }
      } catch {
        fail();
        return;
      }

      animationFrame = requestAnimationFrame(animate);
    };

    const startAnimation = () => {
      if (animationFrame === 0 && renderer && canAnimate()) {
        animationFrame = requestAnimationFrame(animate);
      }
    };

    const initialize = async () => {
      if (renderer || initializing || !canAnimate()) return;
      initializing = true;
      const currentGeneration = generation;

      try {
        const { createSceneRenderer } = await import("./scene-renderer");
        if (!canAnimate() || currentGeneration !== generation) return;
        renderer = createSceneRenderer(canvas, variant);
        resize();
        startAnimation();
      } catch {
        if (currentGeneration === generation) fail();
      } finally {
        if (currentGeneration === generation) initializing = false;
      }
    };

    const reconcile = () => {
      if (reducedMotion || contextLost || failed) {
        disposeRenderer();
        return;
      }
      if (!canAnimate()) {
        stopAnimation();
        return;
      }
      if (renderer) startAnimation();
      else void initialize();
    };

    const onPointerMove = (event: PointerEvent) => {
      if (event.pointerType === "touch") return;
      const bounds = root.getBoundingClientRect();
      if (
        bounds.width <= 0 ||
        bounds.height <= 0 ||
        event.clientX < bounds.left ||
        event.clientX > bounds.right ||
        event.clientY < bounds.top ||
        event.clientY > bounds.bottom
      ) {
        pointer.active = false;
        return;
      }
      pointer.active = true;
      pointer.x = ((event.clientX - bounds.left) / bounds.width) * 2 - 1;
      pointer.y = 1 - ((event.clientY - bounds.top) / bounds.height) * 2;
    };

    const onPointerLeave = () => {
      pointer.active = false;
    };

    const onVisibilityChange = () => {
      documentHidden = document.hidden;
      reconcile();
    };

    const onMotionChange = (event: MediaQueryListEvent) => {
      reducedMotion = event.matches;
      failed = false;
      if (reducedMotion) showFallback();
      reconcile();
    };

    const onContextLost = (event: Event) => {
      event.preventDefault();
      contextLost = true;
      showFallback();
      disposeRenderer();
    };

    const onContextRestored = () => {
      contextLost = false;
      failed = false;
      reconcile();
    };

    resizeObserver = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry) resize(entry.contentRect.width, entry.contentRect.height);
    });
    resizeObserver.observe(root);

    if ("IntersectionObserver" in window) {
      intersectionObserver = new IntersectionObserver(
        ([entry]) => {
          visible = entry?.isIntersecting ?? false;
          reconcile();
        },
        { rootMargin: "80px 0px" },
      );
      intersectionObserver.observe(root);
    } else {
      visible = true;
    }

    surface.addEventListener("pointermove", onPointerMove, { passive: true });
    surface.addEventListener("pointerleave", onPointerLeave);
    document.addEventListener("visibilitychange", onVisibilityChange);
    motionQuery.addEventListener("change", onMotionChange);
    canvas.addEventListener("webglcontextlost", onContextLost);
    canvas.addEventListener("webglcontextrestored", onContextRestored);
    reconcile();

    return () => {
      mounted = false;
      resizeObserver?.disconnect();
      intersectionObserver?.disconnect();
      surface.removeEventListener("pointermove", onPointerMove);
      surface.removeEventListener("pointerleave", onPointerLeave);
      document.removeEventListener("visibilitychange", onVisibilityChange);
      motionQuery.removeEventListener("change", onMotionChange);
      canvas.removeEventListener("webglcontextlost", onContextLost);
      canvas.removeEventListener("webglcontextrestored", onContextRestored);
      disposeRenderer();
    };
  }, [variant]);

  return (
    <div className="hero-scene" data-rendered={rendered} data-variant={variant} ref={rootRef}>
      <div className="hero-scene__fallback">{fallback}</div>
      <canvas aria-hidden="true" className="hero-scene__canvas" ref={canvasRef} tabIndex={-1} />
    </div>
  );
}
