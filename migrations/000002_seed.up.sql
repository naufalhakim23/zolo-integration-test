-- Sample confirmed orders so the service can be exercised end to end without a
-- dashboard.

INSERT OR IGNORE INTO orders (order_id, tenant_id, confirmed_at, confirmed_by_user_id, currency, customer_phone, customer_external_ref)
VALUES
    ('ord_998123', 'tenant_alpha', datetime('now', '-1 hour'),  'usr_4410', 'MYR', '+60 12 345 6789', 'CUST-882'),
    ('ord_998124', 'tenant_alpha', datetime('now', '-2 hours'), 'usr_4410', 'MYR', '+60 12 345 6789', NULL), -- without external ref, so ERP A returns 400
    ('ord_998200', 'tenant_beta',  datetime('now', '-3 hours'), 'usr_7781', 'MYR', '+60 19 888 1122', 'CUST-882'),
    ('ord_998201', 'tenant_beta',  datetime('now', '-4 hours'), 'usr_7781', 'MYR', '+60 19 888 1122', 'WALKIN'),
    ('ord_expired','tenant_beta',  datetime('now', '-30 hours'),'usr_7781', 'MYR', '+60 19 888 1122', 'CUST-901');

-- ord_998123 carries a third line so ERP A's two-line-per-request limit is crossed on
-- the default seed. ord_998200 carries SKU-OUTOFSTOCK so ERP B answers 207.
INSERT OR IGNORE INTO order_items (order_id, line_no, sku, qty, unit_price_cents, discount_bps)
VALUES
    ('ord_998123', 1, 'SKU-MILO-1KG',     10, 1850, 1000),
    ('ord_998123', 2, 'SKU-NESTUM-500G',   5, 1200, 0),
    ('ord_998123', 3, 'SKU-KOPI-200G',     3,  875, 500),
    ('ord_998124', 1, 'SKU-MILO-1KG',      2, 1850, 0),
    ('ord_998200', 1, 'SKU-MILO-1KG',     10, 1850, 1000),
    ('ord_998200', 2, 'SKU-OUTOFSTOCK',    4,  990, 0),
    ('ord_998201', 1, 'SKU-MILO-1KG',      1, 1850, 0),
    ('ord_expired', 1, 'SKU-MILO-1KG',     1, 1850, 0);
