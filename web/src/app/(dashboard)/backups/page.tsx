"use client";

import { useCallback, useEffect, useState } from "react";
import { BackupTable } from "@/components/backups";
import { ErrorAlert } from "@/components/error-alert";
import { Notice } from "@/components/notice";
import { PageHeader } from "@/components/page-header";
import { api, type Backup } from "@/lib/api";
import { message } from "@/lib/errors";

// Every backup on the instance, in one place.
//
// It exists for one row a database's own tab can never show: the ones
// whose database has been **deleted**. Those are kept deliberately —
// deleting a database is exactly the moment its backups matter — and
// without this screen they would be rows nothing on the instance could
// reach, which is the same as not keeping them.
//
// In Platform rather than beside Databases: what is here is not about
// any one database, and half of it is about databases that no longer
// exist.
export default function BackupsPage() {
  const [rows, setRows] = useState<Backup[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    api
      .get<Backup[]>("/backups")
      .then(setRows)
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(load, [load]);

  const orphans = (rows ?? []).filter((b) => !b.database_exists).length;
  const onMachine = (rows ?? []).filter((b) => !b.off_machine).length;

  return (
    <>
      <PageHeader title="Backups" />
      <ErrorAlert error={error} />

      {onMachine > 0 && (
        <Notice tone="warning">
          {onMachine === 1 ? "One backup is" : `${onMachine} backups are`} on this machine's own
          disk. That survives somebody dropping a table and nothing else — not the disk, not the
          box. Pick an object store in the database's own Backups tab to send them somewhere else.
        </Notice>
      )}

      {orphans > 0 && (
        <Notice>
          {orphans === 1 ? "One backup belongs" : `${orphans} backups belong`} to a database that
          has been deleted. They are kept and can be downloaded; restoring one would mean choosing
          which database to load it into, which this release does not do.
        </Notice>
      )}

      <BackupTable rows={rows} onChanged={load} showDatabase />
    </>
  );
}
