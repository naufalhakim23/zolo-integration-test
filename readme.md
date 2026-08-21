# ZOLO Integration Service

On-demand order push from the ZOLO dashboard to tenant ERP backends. An order taker confirms a WhatsApp order, clicks **Confirm & Sync Order**, and this service fetches the confirmed order, maps it to that tenant's ERP format, dispatches it, and records an auditable result.

Go 1.26, Echo v5, SQLite (`modernc.org/sqlite`, pure Go, no CGO).

See [DECISIONS.md](DECISIONS.md) for the architecture rationale and trade-offs.

## Quick start

```bash
cp .env.example .env
make mock-erp    # terminal 1: simulated ERP A + ERP B on :9090
make start       # terminal 2: the integration service on :8080
make demo        # terminal 3: drives every edge case with curl
```

Migrations and seed data run automatically on boot. No database setup needed.

## Tests

```bash
make test        # everything
make test-race   # with the race detector, for the concurrency tests
```

Every test runs against a real SQLite file rather than a mock database. The HTTP-level tests in `internal/app/server` mount the real mock ERP, so chunking and the 429 retry are exercised end to end; the narrower handler tests stub the ERP with a fixed reply to pin one status mapping at a time.

## API

| Method | Endpoint | Purpose |
|---|---|---|
| `POST` | `/api/v1/orders` | Store a confirmed order (the dashboard's "Confirm Order" write) |
| `POST` | `/api/v1/orders/:id/sync` | Push one confirmed order |
| `POST` | `/api/v1/orders/batch-sync` | Push several; one result per order |
| `GET` | `/api/v1/orders/:id/sync-status` | Audit read of the latest attempt |
| `GET` | `/healthz` | Liveness |

Response codes on the sync endpoint:

| Code | Meaning |
|---|---|
| `200` | `SYNCED` |
| `207` | `PARTIAL_SUCCESS` — some line items were rejected, named in the response |
| `409` | A sync for this order is already in flight |
| `422` | Rejected before dispatch (expired, unmappable customer reference, unknown tenant) |
| `404` | No such order |
| `502` | The ERP rejected the order or could not be reached |

`Accept-Language: ms` returns Malay messages; the default is English.

### Confirming an order

`POST /api/v1/orders` takes the ZOLO confirmed-order payload exactly as the brief writes it, decimals included. It is the only place major units enter the system: prices are parsed as text through `big.Rat` and stored as integer cents, and `discount_percent` becomes integer basis points.

```bash
curl -X POST localhost:8080/api/v1/orders -H 'Content-Type: application/json' -d '{
  "order_id": "ord_998123",
  "tenant_id": "tenant_alpha",
  "confirmed_at": "2026-07-22T08:00:00Z",
  "confirmed_by_user_id": "usr_4410",
  "currency": "MYR",
  "customer": { "phone": "+60123456789", "external_ref": "CUST-882" },
  "items": [
    { "sku": "SKU-MILO-1KG",    "qty": 10, "unit_price": 18.5, "discount_percent": 10 },
    { "sku": "SKU-NESTUM-500G", "qty": 5,  "unit_price": 12.0, "discount_percent": 0 }
  ]
}'
```

```json
{
  "status": 201,
  "message": "Order ord_998123 was confirmed and is ready to sync.",
  "data": {
    "order_id": "ord_998123",
    "tenant_id": "tenant_alpha",
    "status": "CONFIRMED",
    "line_count": 2,
    "subtotal_cents": 22650,
    "subtotal": "226.50",
    "currency": "MYR"
  }
}
```

The subtotal comes back in both forms so the caller can see how its decimals were scaled: `18.50 x 0.9 x 10 = 16650`, `12.00 x 5 = 6000`, no drift. Re-posting the same `order_id` is `409 ORDER_EXISTS`, not an update.

### Example

```bash
curl -X POST localhost:8080/api/v1/orders/ord_998200/sync
```

```json
{
  "status": 207,
  "message": "Order ord_998200 was partly accepted. These items were rejected: SKU-OUTOFSTOCK. Please review them and sync again.",
  "data": {
    "order_id": "ord_998200",
    "tenant_id": "tenant_beta",
    "status": "PARTIAL_SUCCESS",
    "message": "Order ord_998200 was partly accepted. These items were rejected: SKU-OUTOFSTOCK. Please review them and sync again.",
    "message_code": "sync.partial",
    "error_code": "ERP_REJECTED",
    "lines": [
      { "sku": "SKU-MILO-1KG", "accepted": true },
      { "sku": "SKU-OUTOFSTOCK", "accepted": false, "reason": "out of stock" }
    ],
    "attempt_count": 1
  }
}
```

Every response carries both a rendered `message` for the order taker and a stable `message_code` / `error_code` pair for the dashboard to branch on.

## Seeded orders

| Order | Tenant | Exercises |
|---|---|---|
| `ord_998123` | alpha | 3 line items, chunked around ERP A's 429; the 16650-cent rounding case |
| `ord_998124` | alpha | Single line; `external_ref` missing, so `Customer_ID` falls back to the stripped phone |
| `ord_998200` | beta | Contains `SKU-OUTOFSTOCK`, so ERP B answers HTTP 207 |
| `ord_998201` | beta | `external_ref: "WALKIN"`, unmappable to an integer `partner_id` |
| `ord_expired` | beta | Confirmed 30 hours ago, past ERP B's 24-hour window |

## Layout

```
cmd/                     wiring; cmd/mockerp is the standalone mock ERP
config/                  env-backed configuration
migrations/              embedded schema + seed, applied on boot
pkg/money/               integer minor units, rounding-safe arithmetic
pkg/driver/              SQLite connection
internal/pkg/            shared options, AppError, constants, i18n catalog
internal/app/
  repository/            data access; repository/model holds the domain types
  tenant/                Mapper interface + registry + alpha + beta   <- the extension point
  erp/                   HTTP transport, retry, backoff
  service/               the tenant-agnostic sync flow
  handler/ server/       HTTP surface
internal/mockerp/        simulated ERP A (429) and ERP B (207)
```

## Docker

```bash
docker compose up --build
```

Starts the mock ERP and the integration service wired together; the service is on `localhost:8080`.

## Adding a tenant

One file in `internal/app/tenant/` implementing `Mapper`, and one `Register` call in `DefaultRegistry`. No change to the sync flow, dispatch, audit or HTTP layers. See DECISIONS.md.
