-- +goose Up
-- Whether a dump actually left this machine, written down rather than
-- worked out from which store it went to.
--
-- It was `object_store_id IS NOT NULL`, and that is wrong for exactly
-- one destination: a **managed** store is a MinIO container this
-- instance runs, and its objects are a bind mount under
-- `<data dir>/objectstores/<id>` — the same disk as the database, the
-- same disk as a local dump. So sending backups there was reported as
-- off the machine, and the coverage report called such a database
-- protected, which is the single lie that screen exists to prevent.
--
-- **A column rather than a join**, for the reason the engine and the
-- version are columns: a store can be deleted, and the foreign key is
-- ON DELETE SET NULL — so a five-month-old row would lose the answer at
-- exactly the moment somebody is trying to work out what they still
-- have. What was true when the dump was taken stays true about it.
ALTER TABLE backups ADD COLUMN off_machine BOOLEAN NOT NULL DEFAULT false;

-- The backfill is the correction. Every existing row that went to an
-- external store did leave; every row on a managed store did not, and
-- was being reported as though it had.
UPDATE backups b SET off_machine = true
FROM object_stores s
WHERE b.object_store_id = s.id AND s.kind = 'external';

-- +goose Down
ALTER TABLE backups DROP COLUMN off_machine;
