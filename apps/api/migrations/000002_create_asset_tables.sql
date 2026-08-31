-- +goose Up
CREATE TABLE assets (
    id CHAR(36) NOT NULL,
    owner_id CHAR(36) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    original_name VARCHAR(255) NOT NULL,
    object_key VARCHAR(512) NOT NULL,
    bucket VARCHAR(128) NOT NULL,
    mime_type VARCHAR(128) NOT NULL,
    size_bytes BIGINT NOT NULL,
    sha256 CHAR(64) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'READY',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_assets_object_key (object_key),
    KEY idx_assets_owner_kind (owner_id, kind),
    KEY idx_assets_status (status),
    CONSTRAINT chk_assets_kind CHECK (kind IN ('IMAGE', 'MODEL')),
    CONSTRAINT chk_assets_status CHECK (status IN ('READY')),
    CONSTRAINT chk_assets_size CHECK (size_bytes > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE product_assets (
    product_id CHAR(36) NOT NULL,
    asset_id CHAR(36) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (product_id, asset_id),
    KEY idx_product_assets_kind (product_id, kind, sort_order),
    CONSTRAINT fk_product_assets_asset FOREIGN KEY (asset_id) REFERENCES assets (id) ON DELETE CASCADE,
    CONSTRAINT chk_product_assets_kind CHECK (kind IN ('IMAGE', 'MODEL'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS product_assets;
DROP TABLE IF EXISTS assets;
