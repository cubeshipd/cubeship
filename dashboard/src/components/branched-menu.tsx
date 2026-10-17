"use client";

import {
  type CSSProperties,
  type ReactNode,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import Link from "@/components/navigation-link";

export type BranchedMenuItem = {
  label: string;
  value?: string;
  icon?: ReactNode;
  children?: { value: string; label: string; icon?: ReactNode }[];
};

const ROW_HEIGHT = 36;
const PAD = 6;

export function BranchedMenu({
  items,
  activeValue,
  onSelect,
  className = "",
}: {
  items: BranchedMenuItem[];
  activeValue: string;
  onSelect?: () => void;
  className?: string;
}) {
  const id = useId();
  const [open, setOpen] = useState(() => new Set(items.map((_, index) => index)));
  const [rowHeight, setRowHeight] = useState(ROW_HEIGHT);
  const active = activeValue;
  const navRef = useRef<HTMLElement>(null);
  const heads = useRef<(HTMLButtonElement | null)[]>([]);
  const markerRef = useRef<HTMLSpanElement>(null);
  const activeSection = items.findIndex((item) =>
    item.children?.some((child) => child.value === active),
  );
  const markerShown = activeSection >= 0 && open.has(activeSection);

  useLayoutEffect(() => {
    const placeMarker = () => {
      const marker = markerRef.current;
      const nav = navRef.current;
      const heading = heads.current[activeSection];
      const measuredRowHeight = Number.parseFloat(
        nav ? getComputedStyle(nav).getPropertyValue("--bm-row") : "",
      );
      if (Number.isFinite(measuredRowHeight)) setRowHeight(measuredRowHeight);
      if (!marker) return;
      if (markerShown && heading)
        marker.style.top = `${heading.offsetTop + (heading.offsetHeight - 16) / 2}px`;
      marker.toggleAttribute("data-on", Boolean(markerShown && heading));
    };

    placeMarker();
    const observer = new ResizeObserver(placeMarker);
    if (navRef.current) observer.observe(navRef.current);
    window.addEventListener("resize", placeMarker);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", placeMarker);
    };
  }, [activeSection, markerShown]);

  return (
    <div className={className}>
      <nav
        ref={navRef}
        aria-label="Main navigation"
        className="branched-menu"
        style={
          {
            "--bm-ink": "var(--foreground)",
            "--bm-accent": "var(--primary)",
            "--bm-line": "var(--border-strong)",
          } as CSSProperties
        }
      >
        <span ref={markerRef} className="branched-menu-marker" aria-hidden="true" />
        {items.map((item, index) => {
          const children = item.children;
          const isOpen = children ? open.has(index) : false;
          const leafValue = item.value ?? item.label;
          const leafActive = !children && leafValue === active;
          const rowY = (childIndex: number) => PAD + childIndex * rowHeight + rowHeight / 2;
          const radius = 10;
          const trunk = 14;
          const endX = 32;
          const branchLength = (childIndex: number) =>
            rowY(childIndex) - radius + (Math.PI * radius) / 2 + (endX - trunk - radius);
          const reach = (childIndex: number) =>
            `M ${trunk} 0 V ${rowY(childIndex) - radius} A ${radius} ${radius} 0 0 0 ${trunk + radius} ${rowY(childIndex)} H ${endX}`;

          return (
            <div
              key={item.value ?? item.label}
              className="branched-menu-section"
              data-open={isOpen ? "" : undefined}
            >
              <button
                ref={(element) => {
                  heads.current[index] = element;
                }}
                type="button"
                className="branched-menu-heading"
                aria-expanded={children ? isOpen : undefined}
                aria-controls={children ? `${id}-section-${index}` : undefined}
                aria-current={leafActive ? "page" : undefined}
                onClick={() => {
                  if (children) {
                    setOpen((current) => {
                      const next = new Set(current);
                      if (next.has(index)) next.delete(index);
                      else next.add(index);
                      return next;
                    });
                  } else {
                    onSelect?.();
                  }
                }}
              >
                <span className="inline-flex items-center gap-2">
                  {item.icon}
                  {item.label}
                </span>
              </button>
              {children && (
                <div id={`${id}-section-${index}`} className="branched-menu-fold">
                  <div className="branched-menu-fold-inner">
                    <div
                      className="branched-menu-children"
                      style={{ height: PAD * 2 + children.length * rowHeight }}
                    >
                      <svg
                        className="branched-menu-lines"
                        width={40}
                        height={PAD * 2 + children.length * rowHeight}
                        aria-hidden="true"
                      >
                        <path
                          className="branched-menu-line"
                          d={`M ${trunk} 0 V ${rowY(children.length - 1) - radius}`}
                        />
                        {children.map((child, childIndex) => (
                          <path
                            key={child.value}
                            className="branched-menu-line"
                            d={reach(childIndex)}
                          />
                        ))}
                        {children.map((child, childIndex) => (
                          <path
                            key={child.value}
                            className="branched-menu-active-line"
                            d={reach(childIndex)}
                            style={{
                              strokeDasharray: branchLength(childIndex),
                              strokeDashoffset:
                                child.value === active ? 0 : branchLength(childIndex),
                            }}
                          />
                        ))}
                      </svg>
                      {children.map((child) => (
                        <Link
                          key={child.value}
                          href={child.value}
                          className="branched-menu-link"
                          aria-current={child.value === active ? "page" : undefined}
                          data-active={child.value === active ? "" : undefined}
                          tabIndex={isOpen ? 0 : -1}
                          onClick={() => {
                            onSelect?.();
                          }}
                        >
                          {child.icon && (
                            <span className="branched-menu-icon" aria-hidden="true">
                              {child.icon}
                            </span>
                          )}
                          <span>{child.label}</span>
                        </Link>
                      ))}
                    </div>
                  </div>
                </div>
              )}
            </div>
          );
        })}
      </nav>
    </div>
  );
}
