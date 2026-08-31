-- +goose Up
CREATE TABLE generation_jobs (
    id CHAR(36) NOT NULL,
    user_id CHAR(36) NOT NULL,
    source_job_id CHAR(36) NULL,
    idempotency_key CHAR(36) NOT NULL,
    request_hash CHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL,
    stage VARCHAR(32) NOT NULL,
    progress TINYINT UNSIGNED NOT NULL DEFAULT 0,
    raw_prompt TEXT NOT NULL,
    optimized_prompt TEXT NULL,
    structured_prompt JSON NULL,
    request_payload JSON NOT NULL,
    rag_context JSON NULL,
    rag_version VARCHAR(64) NULL,
    prompt_template_version VARCHAR(64) NULL,
    provider VARCHAR(32) NOT NULL DEFAULT 'mock',
    provider_model VARCHAR(128) NULL,
    provider_job_id VARCHAR(191) NULL,
    provider_payload JSON NULL,
    attempt INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 3,
    error_code VARCHAR(64) NULL,
    error_message VARCHAR(500) NULL,
    cancel_requested_at DATETIME(3) NULL,
    started_at DATETIME(3) NULL,
    finished_at DATETIME(3) NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_generation_jobs_user_idempotency (user_id, idempotency_key),
    UNIQUE KEY uq_generation_jobs_provider_job (provider_job_id),
    KEY idx_generation_jobs_user_created (user_id, created_at),
    KEY idx_generation_jobs_status_created (status, created_at),
    KEY idx_generation_jobs_source_job (source_job_id),
    CONSTRAINT fk_generation_jobs_source_job FOREIGN KEY (source_job_id) REFERENCES generation_jobs (id) ON DELETE SET NULL,
    CONSTRAINT chk_generation_jobs_status CHECK (status IN ('QUEUED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELED')),
    CONSTRAINT chk_generation_jobs_stage CHECK (stage IN ('QUEUED', 'OPTIMIZING_PROMPT', 'SUBMITTING_PROVIDER', 'GENERATING', 'FETCHING_OUTPUT', 'STORING_OUTPUT', 'COMPLETED')),
    CONSTRAINT chk_generation_jobs_progress CHECK (progress BETWEEN 0 AND 100),
    CONSTRAINT chk_generation_jobs_attempts CHECK (attempt >= 0 AND max_attempts > 0 AND attempt <= max_attempts)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE generation_outputs (
    id CHAR(36) NOT NULL,
    job_id CHAR(36) NOT NULL,
    asset_id CHAR(36) NULL,
    output_type VARCHAR(32) NOT NULL,
    format VARCHAR(16) NOT NULL,
    object_key VARCHAR(512) NULL,
    mime_type VARCHAR(128) NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    sha256 CHAR(64) NOT NULL,
    metadata JSON NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_generation_outputs_identity (job_id, output_type, object_key),
    KEY idx_generation_outputs_job (job_id),
    KEY idx_generation_outputs_asset (asset_id),
    CONSTRAINT fk_generation_outputs_job FOREIGN KEY (job_id) REFERENCES generation_jobs (id) ON DELETE CASCADE,
    CONSTRAINT chk_generation_outputs_size CHECK (size_bytes >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE generation_outbox (
    id CHAR(36) NOT NULL,
    aggregate_id CHAR(36) NOT NULL,
    event_type VARCHAR(64) NOT NULL,
    payload JSON NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'PENDING',
    attempts INT NOT NULL DEFAULT 0,
    available_at DATETIME(3) NOT NULL,
    published_at DATETIME(3) NULL,
    last_error VARCHAR(500) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_generation_outbox_aggregate (aggregate_id),
    KEY idx_generation_outbox_dispatch (status, available_at),
    CONSTRAINT fk_generation_outbox_job FOREIGN KEY (aggregate_id) REFERENCES generation_jobs (id) ON DELETE CASCADE,
    CONSTRAINT chk_generation_outbox_status CHECK (status IN ('PENDING', 'PUBLISHED', 'FAILED')),
    CONSTRAINT chk_generation_outbox_attempts CHECK (attempts >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS generation_outbox;
DROP TABLE IF EXISTS generation_outputs;
DROP TABLE IF EXISTS generation_jobs;
