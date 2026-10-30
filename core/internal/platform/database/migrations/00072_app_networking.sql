-- App container ports and readiness checks are explicit configuration.
-- Preserve the previous domain/published-port settings and legacy probe.
-- +goose Up
ALTER TABLE apps ADD COLUMN internal_ports JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE apps ADD COLUMN health_checks JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE apps a SET internal_ports = COALESCE((
    SELECT jsonb_agg(port ORDER BY port)
    FROM (
        SELECT DISTINCT CASE WHEN d.port = 0 THEN 8080 ELSE d.port END AS port
        FROM app_domains d WHERE d.app_id = a.id
        UNION
        SELECT p.container_port AS port FROM app_tcp_ports p WHERE p.app_id = a.id
        UNION
        SELECT COALESCE((
            SELECT NULLIF(d.port, 0) FROM app_domains d WHERE d.app_id = a.id AND d.port > 0 ORDER BY d.id LIMIT 1
        ), (
            SELECT p.container_port FROM app_tcp_ports p WHERE p.app_id = a.id ORDER BY p.id LIMIT 1
        ), 8080) WHERE a.health_path <> ''
    ) ports
), '[]'::jsonb);

UPDATE apps a SET health_checks = jsonb_build_array(jsonb_build_object(
    'path', a.health_path,
    'port', COALESCE((
        SELECT NULLIF(d.port, 0) FROM app_domains d WHERE d.app_id = a.id AND d.port > 0 ORDER BY d.id LIMIT 1
    ), (
        SELECT p.container_port FROM app_tcp_ports p WHERE p.app_id = a.id ORDER BY p.id LIMIT 1
    ), 8080),
    'initial_delay_seconds', 0,
    'period_seconds', 10,
    'timeout_seconds', 5,
    'failure_threshold', 3,
    'success_threshold', 1
)) WHERE a.health_path <> '';

-- +goose Down
ALTER TABLE apps DROP COLUMN health_checks;
ALTER TABLE apps DROP COLUMN internal_ports;
