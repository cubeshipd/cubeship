import type { ReactNode } from "react";
import { ProjectWorkspace } from "@/components/project-workspace";

export default function ProjectsLayout({ children }: { children: ReactNode }) {
  return <ProjectWorkspace>{children}</ProjectWorkspace>;
}
