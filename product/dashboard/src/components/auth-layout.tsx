import type { ReactNode } from "react";
import { Wordmark } from "@/components/brand";

// Sign-in and setup share the same product identity and form layout.
export function AuthLayout({
  title,
  description,
  children,
  footer,
}: {
  title: string;
  description: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <div className="auth-layout">
      <aside className="auth-brand-panel">
        <Wordmark className="relative text-sm" markClassName="size-7" />
        <div className="auth-brand-copy">
          <h2>
            Your infrastructure.
            <br />
            Ready to ship.
          </h2>
          <p>
            Deploy apps, connect your data and keep everything in reach. On servers you control.
          </p>
        </div>
        <InfrastructureMark />
        <div className="auth-brand-footer">
          <span>Open source.</span>
          <span>Self hosted.</span>
          <span>All yours.</span>
        </div>
      </aside>
      <main className="auth-main">
        <div className="auth-form-wrap">
          <Wordmark className="auth-mobile-brand text-sm" markClassName="size-6" />
          <header className="auth-form-heading">
            <h1>{title}</h1>
            <p>{description}</p>
          </header>
          {children}
          {footer && <div className="auth-form-footer">{footer}</div>}
        </div>
      </main>
    </div>
  );
}

function InfrastructureMark() {
  const dots: { key: string; x: number; y: number; opacity: number }[] = [];
  for (let face = 0; face < 3; face++) {
    for (let row = 0; row <= 20; row++) {
      for (let col = 0; col <= 20; col++) {
        const u = col / 20;
        const v = row / 20;
        const x = face === 0 ? 270 + (u - v) * 170 : face === 1 ? 100 + u * 170 : 270 + u * 170;
        const y =
          face === 0
            ? 14 + (u + v) * 72
            : face === 1
              ? 86 + u * 72 + v * 166
              : 158 - u * 72 + v * 166;
        dots.push({
          key: `${face}-${row}-${col}`,
          x,
          y,
          opacity:
            row === 0 || col === 0 || row === 20 || col === 20
              ? 0.8
              : 0.22 + ((row + col) % 5) * 0.08,
        });
      }
    }
  }
  return (
    <svg viewBox="0 0 540 350" fill="none" className="auth-art" aria-hidden="true">
      {dots.map((dot) => (
        <circle
          key={dot.key}
          cx={dot.x}
          cy={dot.y}
          r="1"
          fill="currentColor"
          opacity={dot.opacity}
        />
      ))}
      <path
        d="M0 242H110L270 312L435 240H540M0 132H102L270 202L438 131H540"
        stroke="currentColor"
        strokeOpacity=".22"
        strokeWidth=".7"
      />
      <path d="M8 242H58M468 131H498" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  );
}
