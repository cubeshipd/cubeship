import {
  BoxIcon,
  ContainerIcon,
  DatabaseIcon,
  FolderTreeIcon,
  GlobeIcon,
  HardDriveIcon,
  KeyRoundIcon,
  LayersIcon,
  NetworkIcon,
  PackageIcon,
  ServerIcon,
  UsersIcon,
} from "lucide-react";

// One mark per kind of thing, for everywhere a kind is shown without
// its own screen around it: the menus in the rail, and both halves of
// the command palette.
//
// **One map, because a database has to look like a database twice.**
// The rail and the palette had their own, and two lists of the same
// nine facts is one list that goes stale — the day something is added
// to one of them, the other quietly keeps a different opinion about
// what a bucket looks like.
//
// Where the sidebar already lists a thing, this is the sidebar's own
// icon: projects, databases, stores, registries, DNS, credentials,
// servers and users are all entries there, and a second mark for one of
// them would be a second name for it. The four that are not in the
// sidebar — an environment, an app, a bucket, a zone — are chosen to
// sit apart from the ones that are.
export type Mark =
  | "project"
  | "environment"
  | "app"
  | "database"
  | "store"
  | "bucket"
  | "registry"
  | "dns"
  | "zone"
  | "credential"
  | "server"
  | "user";

export const MARKS: Record<Mark, typeof BoxIcon> = {
  project: FolderTreeIcon,
  environment: LayersIcon,
  app: BoxIcon,
  database: DatabaseIcon,
  store: HardDriveIcon,
  bucket: PackageIcon,
  registry: ContainerIcon,
  dns: GlobeIcon,
  zone: NetworkIcon,
  credential: KeyRoundIcon,
  server: ServerIcon,
  user: UsersIcon,
};
