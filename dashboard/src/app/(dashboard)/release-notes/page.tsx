"use client";

import { useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { PageLoading } from "@/components/page-loading";
import { Notes } from "@/components/release-notes";
import { SectionHeader } from "@/components/section-header";
import { Card, CardContent } from "@/components/ui/card";
import { api, type Releases } from "@/lib/api";
import { message } from "@/lib/errors";

export default function ReleaseNotesPage() {
  const [releases, setReleases] = useState<Releases | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .get<Releases>("/releases")
      .then(setReleases)
      .catch((err) => setError(message(err)));
  }, []);

  return (
    <>
      <SectionHeader
        page
        title="Release notes"
        sub={
          releases?.version
            ? `Changes up to Cubeship ${releases.version}.`
            : "What's new in Cubeship."
        }
      />
      <ErrorAlert error={error} />
      {!releases && !error && <PageLoading kind="form" label="Loading release notes" />}
      {releases?.notes.length === 0 && (
        <p className="text-sm text-muted-foreground">No release notes for this build yet.</p>
      )}
      <div className="max-w-4xl space-y-6">
        {releases?.notes.map((note) => (
          <Card key={note.version}>
            <CardContent className="space-y-5 [&>.dashboard-section-header]:mb-0">
              <SectionHeader
                title={`Cubeship ${note.version}`}
                sub={`${note.date} · ${note.summary}`}
              />
              <Notes body={note.body} />
            </CardContent>
          </Card>
        ))}
      </div>
    </>
  );
}
