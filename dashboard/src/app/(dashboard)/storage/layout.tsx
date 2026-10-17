import type { ReactNode } from "react";
import { StorageWorkspace } from "@/components/storage-workspace";

export default function StorageLayout({ children }: { children: ReactNode }) {
  return <StorageWorkspace>{children}</StorageWorkspace>;
}
