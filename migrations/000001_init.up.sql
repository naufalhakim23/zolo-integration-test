-- This is table for the order
CREATE TABLE IF NOT EXISTS orders (
    order_id             TEXT PRIMARY KEY,
    tenant_id            TEXT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'CONFIRMED',
    confirmed_at         TIMESTAMP NOT NULL,
    confirmed_by_user_id TEXT NOT NULL,
    currency             TEXT NOT NULL,
    customer_phone       TEXT,
    customer_external_ref TEXT,
    created_at           TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_orders_tenant ON orders (tenant_id);

-- This is table for the order items. Each order can have multiple items, 
-- and each item has a line number to identify it within the order. 
-- The unit price is stored in cents to avoid floating point issues, 
-- and discounts are stored in basis points (bps) to allow for precise percentage discounts.
CREATE TABLE IF NOT EXISTS order_items (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id         TEXT NOT NULL REFERENCES orders (order_id) ON DELETE CASCADE,
    line_no          INTEGER NOT NULL, -- line number within the order, starting from 1
    sku              TEXT NOT NULL,
    qty              INTEGER NOT NULL CHECK (qty > 0),
    unit_price_cents INTEGER NOT NULL CHECK (unit_price_cents >= 0),
    discount_bps     INTEGER NOT NULL DEFAULT 0 CHECK (discount_bps BETWEEN 0 AND 10000),
    UNIQUE (order_id, line_no)
);

-- This is table for the sync attempts. 
-- Each attempt is associated with an order and a tenant, and has a unique idempotency 
-- key to prevent duplicate processing. 
-- The status of the attempt can be PENDING, SYNCED, PARTIAL_SUCCESS, or FAILED. 
CREATE TABLE IF NOT EXISTS sync_attempts (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id          TEXT NOT NULL REFERENCES orders (order_id) ON DELETE CASCADE,
    tenant_id         TEXT NOT NULL,
    idempotency_key   TEXT NOT NULL UNIQUE,
    status            TEXT NOT NULL CHECK (status IN ('PENDING', 'SYNCED', 'PARTIAL_SUCCESS', 'FAILED')),
    error_code        TEXT, -- error code returned by the ERP system, if any
    message_code      TEXT, -- message code returned by the ERP system, if any
    message_params    TEXT, -- message parameters returned by the ERP system, if any
    request_snapshot  TEXT, -- snapshot of the request payload for debugging purposes
    response_snapshot TEXT, -- snapshot of the response payload for debugging purposes
    attempt_count     INTEGER NOT NULL DEFAULT 0,
    created_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sync_attempts_order ON sync_attempts (order_id);

-- Per-line outcome, which is what makes an HTTP 207 reportable: the dashboard
-- needs to know which SKU failed, not just that the order was partial.
CREATE TABLE IF NOT EXISTS sync_line_results (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_attempt_id INTEGER NOT NULL REFERENCES sync_attempts (id) ON DELETE CASCADE,
    sku             TEXT NOT NULL,
    accepted        INTEGER NOT NULL CHECK (accepted IN (0, 1)),
    reason          TEXT,
    UNIQUE (sync_attempt_id, sku)
);
