-- +goose Up
CREATE TABLE audit_logs (
    id CHAR(36) NOT NULL,
    actor_id CHAR(36) NOT NULL,
    action VARCHAR(64) NOT NULL,
    target_type VARCHAR(32) NOT NULL,
    target_id VARCHAR(64) NOT NULL,
    request_id VARCHAR(128) NOT NULL,
    before_state JSON NULL,
    after_state JSON NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_audit_logs_actor_created (actor_id, created_at),
    KEY idx_audit_logs_target_created (target_type, target_id, created_at),
    KEY idx_audit_logs_request (request_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS audit_logs;
