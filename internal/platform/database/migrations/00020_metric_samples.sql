-- +goose Up
-- +goose StatementBegin

-- What each container is using, sampled on a timer.
--
-- In Postgres because that is the only store this instance has.
-- Cubeship runs on one VPS with no external services, so "add
-- Prometheus" is not a smaller answer than a table — it is a second
-- thing to install, run, back up and reach.
--
-- One table for every kind of subject rather than one per module: an
-- app and a datastore are both a container with a CPU and a resident
-- set, and the chart drawn from these rows is the same chart. `kind`
-- is what tells them apart, and it is deliberately not a foreign key —
-- there is no one table to point at, and a sample outliving the thing
-- it measured by a few minutes is harmless where a cascade across two
-- parents would be a trigger.
CREATE TABLE metric_samples (
    id         BIGSERIAL PRIMARY KEY,
    -- "app" or "datastore".
    kind       TEXT        NOT NULL,
    subject_id BIGINT      NOT NULL,
    at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Percentage of one core, so 250 means two and a half cores. Not
    -- capped at 100: a container using four cores on an eight-core box
    -- is a fact worth seeing, and rescaling it to "50% of the machine"
    -- would hide it behind the size of the machine.
    cpu_percent      DOUBLE PRECISION NOT NULL,
    memory_bytes     BIGINT           NOT NULL,
    -- The cgroup's ceiling, which is the host's memory for a container
    -- with no limit of its own. Stored per sample rather than derived,
    -- because it changes when a limit is set and a chart of the past
    -- should not be redrawn against today's ceiling.
    memory_limit_bytes BIGINT         NOT NULL
);

-- Every read is "one subject, recent first", and every delete is "older
-- than". One index serves both.
CREATE INDEX metric_samples_subject_at ON metric_samples (kind, subject_id, at DESC);
CREATE INDEX metric_samples_at ON metric_samples (at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE metric_samples;
-- +goose StatementEnd
