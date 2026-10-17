import type { ReactNode } from "react";

// The first section can also serve as the page title when the rail is absent.
export function SectionHeader({
  title,
  sub,
  actions,
  page = false,
}: {
  title: ReactNode;
  sub?: ReactNode;
  actions?: ReactNode;
  page?: boolean;
}) {
  return (
    <div
      className={
        page ? "dashboard-section-header dashboard-page-header" : "dashboard-section-header"
      }
    >
      <div className="min-w-0">
        <h2>{title}</h2>
        {sub && <p>{sub}</p>}
      </div>
      {actions && <div className="dashboard-section-actions">{actions}</div>}
    </div>
  );
}
