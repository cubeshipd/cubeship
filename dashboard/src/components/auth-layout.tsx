import type { ReactNode } from "react";
import { Wordmark } from "@/components/brand";
import GradientASCII from "@/components/gradient-ascii";

// Sign-in and setup share the same product identity and form layout.
export function AuthLayout({
  title,
  description,
  children,
  footer,
  variant = "split",
}: {
  title: string;
  description: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  variant?: "split" | "overlay";
}) {
  return (
    <div className={`auth-layout${variant === "overlay" ? " auth-layout-overlay" : ""}`}>
      <aside className="auth-brand-panel">
        <div className="auth-ascii-bg" aria-hidden="true">
          <GradientASCII
            colors={["#0b2430", "#14515c", "#19828b", "#36b5bb"]}
            shape="horizon"
            characters=" .:-=+*#@"
            cellSize={18}
            density={0.38}
            glyphOpacity={0.52}
            glow={0.1}
            flow={0.1}
            speed={0.1}
            grain={0.01}
            mouseInteraction={false}
            clickEffect="none"
          />
        </div>
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
        <div className="auth-brand-footer">
          <span>Open source.</span>
          <span>Self hosted.</span>
          <span>All yours.</span>
        </div>
      </aside>
      <main className={`auth-main${variant === "overlay" ? " auth-main-overlay" : ""}`}>
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
