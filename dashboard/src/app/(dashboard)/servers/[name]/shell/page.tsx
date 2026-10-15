"use client";

import { use } from "react";
import { Notice } from "@/components/notice";
import { useSession } from "@/components/session-context";
import { TerminalPanel } from "@/components/terminal-panel";
import { canRootShell } from "@/lib/api";

// A root shell on one of the instance's machines.
//
// A page rather than a dialog over the servers table: it is somewhere you
// go and stay, and a URL is what lets it sit in a tab of its own beside
// the dashboard. The password is asked for inside the panel, because the
// daemon asks for it again on the connection itself — this only decides
// who is shown the form.
export default function ServerShell({ params }: PageProps<"/servers/[name]/shell">) {
  const { name } = use(params);
  const me = useSession();
  if (!canRootShell(me)) {
    return <Notice>Only an administrator can open a root shell on a server.</Notice>;
  }
  return <TerminalPanel path={`/nodes/${encodeURIComponent(name)}/shell`} password />;
}
