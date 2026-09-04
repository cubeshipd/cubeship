"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { api, type App, type SetupStatus } from "@/lib/api";
import { Card, PageHeader, Shell, Status, message } from "@/components/ui";

// The first page anyone opens, so it is also the gate: an unclaimed
// instance goes to setup before anything else renders.
export default function Home() {
  const router = useRouter();
  const [checked, setChecked] = useState(false);

  useEffect(() => {
    api
      .get<SetupStatus>("/setup")
      .then((s) => (s.needed ? router.replace("/setup") : setChecked(true)))
      .catch(() => setChecked(true));
  }, [router]);

  if (!checked) return null;
  return (
    <Shell>
      <Apps />
    </Shell>
  );
}

function Apps() {
  const [apps, setApps] = useState<App[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.get<App[]>("/apps").then(setApps).catch((e) => setError(message(e)));
  }, []);

  return (
    <>
      <div className="mb-6 flex items-start justify-between">
        <PageHeader title="Apps" sub="Everything running on this instance." />
        <Link
          href="/apps/new"
          className="rounded-md border border-brand bg-brand px-3.5 py-2 text-sm text-white hover:opacity-90"
        >
          New app
        </Link>
      </div>

      {error && <Card>{error}</Card>}

      {apps?.length === 0 && (
        <Card>
          <p className="text-sm text-muted">
            No apps yet. <Link href="/apps/new">Create one</Link> — you push an image to it and it
            deploys.
          </p>
        </Card>
      )}

      {apps && apps.length > 0 && (
        <Card className="p-0">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-line text-xs text-muted">
                <th className="p-3 text-left font-medium">App</th>
                <th className="p-3 text-left font-medium">Domain</th>
                <th className="p-3 text-left font-medium">Status</th>
              </tr>
            </thead>
            <tbody>
              {apps.map((a) => (
                <tr key={a.reference} className="border-b border-line last:border-0">
                  <td className="p-3">
                    <Link href={`/apps?ref=${a.reference}`} className="font-mono text-xs">
                      {a.reference}
                    </Link>
                  </td>
                  <td className="p-3 text-muted">{a.domain}</td>
                  <td className="p-3">
                    <Status value={a.status} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}
    </>
  );
}
