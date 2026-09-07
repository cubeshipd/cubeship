-- +goose Up

-- What the machine itself is doing, one row per collection pass.
--
-- Its own table rather than a fourth kind in metric_samples. That one
-- is per container and answers one question about each — a CPU and a
-- resident set — and the host is a different set of measurements: a
-- disk it is filling and a wire it is moving bytes over, neither of
-- which a container has. Folding them in would be four columns that are
-- NULL for every row but one kind's.
--
-- There is no subject id, because there is one machine. Cubeship runs
-- one instance on one VPS, and a table keyed by something with one
-- value is a join nobody ever needs.
CREATE TABLE host_samples (
    at                 TIMESTAMPTZ      NOT NULL,
    -- Percent of the whole machine, not of one core: 100 is every core
    -- busy. The opposite convention from metric_samples, and it is the
    -- right one on each side — a container's share of a host is what
    -- 250% means there, and "how much of this box is left" is what is
    -- being asked here.
    cpu_percent        DOUBLE PRECISION NOT NULL,
    memory_bytes       BIGINT           NOT NULL,
    memory_total_bytes BIGINT           NOT NULL,
    -- The filesystem the data directory is on, which is the disk
    -- everything this instance keeps lives on: images, build cache,
    -- databases, buckets.
    disk_bytes         BIGINT           NOT NULL,
    disk_total_bytes   BIGINT           NOT NULL,
    -- Bytes per second over the host's own interfaces, averaged across
    -- the interval. NULL rather than 0 when this daemon cannot see
    -- them: a container's /proc/net/dev is its own network namespace,
    -- so without the host's procfs mounted there is no answer here at
    -- all — and zero is an answer.
    rx_bytes_per_sec   DOUBLE PRECISION,
    tx_bytes_per_sec   DOUBLE PRECISION
);

CREATE INDEX host_samples_at ON host_samples (at DESC);

-- +goose Down
DROP TABLE host_samples;
