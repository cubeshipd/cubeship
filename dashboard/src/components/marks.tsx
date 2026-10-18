import {
  BoxIcon,
  ContainerIcon,
  DatabaseIcon,
  FolderTreeIcon,
  GlobeIcon,
  HardDriveIcon,
  KeyRoundIcon,
  ServerIcon,
  UsersIcon,
} from "lucide-react";

// One mark per kind of thing shown in the command palette.
// Where the sidebar also lists it, use the same icon.
export type Mark =
  | "project"
  | "app"
  | "database"
  | "store"
  | "registry"
  | "dns"
  | "credential"
  | "server"
  | "user";

export const MARKS: Record<Mark, typeof BoxIcon> = {
  project: FolderTreeIcon,
  app: BoxIcon,
  database: DatabaseIcon,
  store: HardDriveIcon,
  registry: ContainerIcon,
  dns: GlobeIcon,
  credential: KeyRoundIcon,
  server: ServerIcon,
  user: UsersIcon,
};
