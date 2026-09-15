import type { ReactNode } from "react";

// Section titles sit below the shared path rail. Sentence case and a
// larger type size establish the reading hierarchy without a second page title.
export function SectionHeader({
  title,
  sub,
  actions,
}: {
  title: ReactNode;
  sub?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div className="dashboard-section-header">
      <div className="min-w-0">
        <h2>{title}</h2>
        {sub && <p>{sub}</p>}
      </div>
      {actions && <div className="dashboard-section-actions">{actions}</div>}
    </div>
  );
}
