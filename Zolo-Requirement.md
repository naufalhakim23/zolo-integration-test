# Welcome Integration-focused Product Exercise

```
TIMEBOX:    3-4 hours max. We mean it! Set a timer and hard-stop at 4 hours ⏱
LANGUAGES:  TypeScript, Go
RUNTIMES:   NodeJS, Deno, Bun, or Go
FRAMEWORKS: Express, Koa, Nestjs, Hono, net/http, Gin, Echo, ...
TESTS:      expected — cover the edge cases described below
DOCS:       a short DECISIONS.md is required (see Deliverables)
```

## Overview

This exercise is to implement the best possible solution to the problem below in the time allotted. We're evaluating your ability to take a set of integration requirements — the kind riddled with real-world edge cases, not happy-path demos — and turn them into a holistic solution that demonstrates craftsmanship, thoughtfulness, and good architectural design. This is **NOT** a test of how well you know any particular framework, nor should you try to impress us with overly clever and obtuse solutions. If you want to impress us, build something that is correct, debuggable, and easy to extend to a third tenant :smiley:.

Ideally your solution would have some way to run locally (mock ERP included) and be exercised via tests so we can fully evaluate your work.

## Context & Scenario

ZOLO users (order takers) process incoming WhatsApp orders on our dashboard. When an order taker verifies the items, quantities, and pricing, they click **"Confirm & Sync Order"**.

Your task is to build the **Integration Service** that handles this on-demand order push (`POST /orders/:id/sync`). The service fetches the confirmed order from ZOLO's local database, transforms it into the tenant's specific ERP format, dispatches it to a mock ERP backend, handles partial/full failures, and updates ZOLO's internal audit state.

## Core Technical Requirements

Implement a service in TypeScript (Node.js) or Go that exposes an API endpoint (`POST /orders/:id/sync` or `POST /orders/batch-sync`):

1. **Fetch Confirmed Order Data** — Retrieve the target confirmed order(s) from a local database or mock staging state.
2. **Apply Tenant-Specific Transformations** — Execute mapping logic according to the tenant's ERP rules (Tenant A vs. Tenant B).
3. **Dispatch to Mock ERP Endpoints** via HTTP.
4. **Update Sync State & Return Audit Output** — Atomically mark the order as `SYNCED`, `PARTIAL_SUCCESS`, or `FAILED` with friendly, translated error messages, and return the result to the caller.

## The Test Bench: Client Schemas & Edge Cases

### Input Data (ZOLO Confirmed Order Payload)

When an order taker clicks "Confirm Order", your endpoint processes an order payload structured as follows:

```json
{
  "order_id": "ord_998123",
  "tenant_id": "tenant_alpha",
  "confirmed_at": "2026-07-22T08:00:00Z",
  "confirmed_by_user_id": "usr_4410",
  "currency": "MYR",
  "customer": {
    "phone": "+60123456789",
    "external_ref": "CUST-882"
  },
  "items": [
    {
      "sku": "SKU-MILO-1KG",
      "qty": 10,
      "unit_price": 18.5,
      "discount_percent": 10
    },
    {
      "sku": "SKU-NESTUM-500G",
      "qty": 5,
      "unit_price": 12.0,
      "discount_percent": 0
    }
  ]
}
```

### Tenant A Mapping Rules (`tenant_alpha` → Mock ERP A)

**Protocol:** `POST /api/v1/sap-adapter/orders`

**Data Transformations:**

- `Customer_ID`: Strip `+` and spaces from the phone number if `external_ref` is missing. Otherwise, use `external_ref`.
- `Line_Items`: ERP A requires prices in **Cents** (integers), not floating decimals.
- `Discount_Calculation`: Apply `discount_percent` to each line item _before_ converting to cents.

**Edge Cases (must handle correctly):**

- **Floating Point Rounding Hazard**: `18.50 × (1 - 0.10) × 10 items = 166.50 → 16650 cents`. Ensure the math uses explicit integer/rounding-safe logic to prevent 1-cent drift errors.
- **Idempotency Guarantee**: If the order taker double-clicks "Confirm Order", the service must prevent duplicate submissions to ERP A. Generate a deterministic idempotency key header `X-Idempotency-Key: SHA256(order_id + tenant_id)`.
- **Simulated ERP Rate Limit / Chunking**: ERP A fails with HTTP `429 Too Many Requests` if an order contains more than 2 line items in a single HTTP request. Chunk line items and/or implement exponential backoff retries.

### Tenant B Mapping Rules (`tenant_beta` → Mock ERP B)

**Protocol:** `POST /api/v2/odoo-adapter/sales-order`

**Data Transformations:**

- `partner_id`: Must be an integer extracted from `external_ref` (e.g. `"CUST-882"` → `882`). If extraction fails, reject the sync attempt pre-flight with a `VALIDATION_ERROR` without calling ERP B.
- `order_lines`: Tax is not included in ZOLO's input. ERP B requires a static **8% SST** tax added to the header total, but line items must remain tax-exclusive.

**Edge Cases (must handle correctly):**

- **Temporal State Dependency**: If the order's `confirmed_at` timestamp is older than 24 hours, ERP B rejects the sync with HTTP `400` (`ORDER_EXPIRED`). Catch this early, in pre-validation.
- **Partial Line-Item Multi-Status (HTTP 207)**: If ERP B returns `HTTP 207 Multi-Status` indicating one line item succeeded and one failed (e.g. out of stock in ERP B), the service must **not** mark the entire order as `FAILED`. Record a `PARTIAL_SUCCESS` state in local storage and return a localized, friendly message identifying the specific failing SKU to the user dashboard.

## Deliverables & Acceptance Criteria

- [ ] **Working Sync Endpoint** — An API (`POST /orders/:id/sync`) that handles on-demand push requests triggered by user confirmation.
- [ ] **Mock Server / Interceptor** — A mock server simulating ERP A and ERP B, including the rate-limiting (429) and multi-status (207) scenarios.
- [ ] **Audit & State Storage** — SQLite or in-memory database recording sync status (`PENDING`, `SYNCED`, `PARTIAL_SUCCESS`, `FAILED`), payload snapshots, idempotency keys, and friendly error messages.
- [ ] **Unit & Integration Tests** — Automated tests covering integer rounding math, idempotency key generation, multi-status HTTP 207 handling, and double-click submission prevention.
- [ ] **DECISIONS.md** — Explaining mapping architecture, extensibility for a hypothetical Tenant C, floating-point math mitigation, and how concurrency/double-clicks are safely managed.

## Evaluation Criteria

- _Correctness_: Are the rounding, idempotency, chunking, expiry, and multi-status edge cases all handled as specified?
- _Robustness_: Does the service degrade gracefully (partial success, friendly errors) instead of crashing or mislabeling state?
- _Architecture & Code Quality_: Is the tenant-mapping logic pluggable enough that adding Tenant C wouldn't require rewriting the core sync flow?
- _Test Coverage_: Do the tests actually exercise the AI-trap edge cases, not just the happy path?
- _Documentation_: Does `DECISIONS.md` clearly explain the trade-offs made under time pressure?

## Submitting your exercise

1. See [instructions for submitting your work](https://github.com/zolomart-dev/hiring-exercises/blob/master/README.md#general-instructions)