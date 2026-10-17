import type { ReactNode } from "react";
import { DatabaseWorkspace } from "@/components/database-workspace";

export default function DatabasesLayout({ children }: { children: ReactNode }) {
  return <DatabaseWorkspace>{children}</DatabaseWorkspace>;
}
