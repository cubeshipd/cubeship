-- +goose Up

-- When this instance changes an app's replica count on its own.
--
-- **Off is `autoscale_max = 0`**, which is every app that exists. The
-- other two only mean anything alongside it: a floor the app never goes
-- below, and the CPU each copy should be sitting at — where 100 is one
-- core, the same scale every container chart on this instance is drawn
-- on.
--
-- `autoscaled_at` is when this last changed the count, and it is what a
-- cooldown is measured from. Stored rather than held in memory because
-- a daemon restart would otherwise be a free pass to act again
-- immediately, and a restart is exactly what happens during an upgrade
-- — which is when a rollout is already moving load around.
ALTER TABLE apps
    ADD COLUMN autoscale_min INT NOT NULL DEFAULT 0,
    ADD COLUMN autoscale_max INT NOT NULL DEFAULT 0,
    ADD COLUMN autoscale_cpu NUMERIC(6, 2) NOT NULL DEFAULT 0,
    ADD COLUMN autoscaled_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE apps
    DROP COLUMN autoscale_min, DROP COLUMN autoscale_max,
    DROP COLUMN autoscale_cpu, DROP COLUMN autoscaled_at;
