# Decisions

Why this service is shaped the way it is, and what I traded away to get there.

## Mapping architecture

The sync flow is one function, and it contains no tenant conditionals:

```
fetch order -> resolve mapper -> pre-validate -> claim -> build -> dispatch -> interpret -> persist
```

Everything tenant-specific sits behind one interface (`internal/app/tenant`):

```go
type Mapper interface {
	TenantID() string
	PreValidate(order model.Order, now time.Time) *pkg.AppError
	Build(order model.Order) ([]erp.Request, *pkg.AppError)
	Interpret(order model.Order, responses []erp.Response) Outcome
}
```

The three methods split along the boundaries that actually differ between ERPs:

- **PreValidate** answers "would this ERP refuse the order outright?" ERP B rejects orders older than 24 hours and needs an integer partner id; ERP A needs neither. Catching it here means no wasted round trip and no consumed idempotency key.
- **Build** answers "what goes on the wire?" and returns a *slice* of requests. That is what makes chunking a tenant-local concern: ERP A splits at two line items, ERP B sends one request. The dispatcher does not know or care which happened.
- **Interpret** answers "what did that reply mean?" ERP B's HTTP 207 becomes `PARTIAL_SUCCESS` with per-SKU detail; ERP A's per-chunk failures become the same shape. Both produce a tenant-agnostic `Outcome` that the service persists without inspecting.

Transport (`internal/app/erp`) owns retries, backoff, timeouts and encoding. Adapters never see an `http.Client`. That separation is why a tenant author cannot accidentally get the retry policy wrong.

**Tradeoff**: `Build` returning `[]erp.Request` means every tenant pays for a slice even though only ERP A chunks. The alternative, a single request plus a separate `ChunkSize()` knob, pushes chunking into the core flow and makes it the framework's problem, which is exactly backwards when the limit is a property of one ERP. I took the small allocation.

## Adding Tenant C

One file and one line. Nothing in `service/`, `handler/`, `repository/`, `erp/` or the HTTP surface changes:

```go
// internal/app/tenant/gamma.go
type Gamma struct{ baseURL string }

func (g *Gamma) TenantID() string { return "tenant_gamma" }
func (g *Gamma) PreValidate(order model.Order, now time.Time) *pkg.AppError { ... }
func (g *Gamma) Build(order model.Order) ([]erp.Request, *pkg.AppError) { ... }
func (g *Gamma) Interpret(order model.Order, responses []erp.Response) Outcome { ... }
```

```go
// internal/app/tenant/registry.go, in DefaultRegistry
r.Register(NewGamma(cfg.GammaBaseURL))
```

An unregistered `tenant_id` fails with `UNKNOWN_TENANT` and never reaches the network, so a misconfigured tenant is a clear error rather than a silent no-op.

What a new tenant inherits for free: idempotency, the claim/replay concurrency rules, retry and backoff, request and response snapshots, per-line audit rows, localized messages, batch sync, and the status endpoint.

## Floating-point math

**No `float64` anywhere in the money path.** `pkg/money` holds `Amount`, an `int64` of minor units (cents).

- Decimal input is parsed as *text* through `math/big.Rat`, never through a float. `18.5` has no exact binary representation, and the classic failure is `2.675 * 100 == 267.49999999999997`, which truncates to 267 instead of 268.
- Rounding is **half away from zero**, applied via `big.Rat` so the halfway test is exact rather than a float comparison.
- Discount is applied to the already-scaled line total and rounded exactly **once**: `round(unit_cents x qty x (10000 - discount_bps) / 10000)`. Rounding per unit and then multiplying is what produces the 1-cent drift the exercise warns about. The worked example is a test: `1850 x 10 x 0.9 = 16650` exactly.
- Discounts are stored as integer basis points, not percentages, so `discount_percent: 10` never becomes `0.1` in a float.
- Prices are `INTEGER` at rest too. Nothing can reintroduce drift by round-tripping through storage.
- `Amount`'s JSON codec is symmetric: it marshals and unmarshals minor units. Decimal ingestion lives in a separately named `money.Decimal`, so the scaling is visible in the type rather than hidden in a codec. An asymmetric codec here would multiply a value by 100 every time it round-tripped through an audit snapshot, silently.

## Concurrency and double-clicks

The idempotency key alone does not solve this. `SHA256(order_id + tenant_id)` makes a repeat *recognisable*, but two requests already in flight have not written anything yet, so there is nothing to recognise. The key is necessary and not sufficient.

The actual control is a **claim row** with a `UNIQUE` constraint:

```sql
INSERT INTO sync_attempts (order_id, tenant_id, idempotency_key, status, attempt_count)
VALUES (?, ?, ?, 'PENDING', 1)
ON CONFLICT (idempotency_key) DO NOTHING
```

Both racing requests run this. The database serialises them and exactly one reports a row affected; that one dispatches. The loser reads the row it collided with and:

- `PENDING` -> HTTP 409 with "already being synced, please wait"
- terminal -> replays the stored result, flagged `"replayed": true`, with no ERP call

Three rules govern re-claiming a row that already exists:

| Existing state | Re-dispatch? | Why |
|---|---|---|
| `SYNCED` | **Never** | This is the idempotency guarantee. The order is in the ERP; no amount of clicking sends it again. |
| `FAILED` / `PARTIAL_SUCCESS` | Yes | The partial-success message tells the order taker to fix the items and sync again. Refusing that would make the instruction a lie. |
| `PENDING`, older than 2 minutes | Yes | A process that died mid-dispatch would otherwise block the order forever. |

Every predicate lives *inside* the `UPDATE`, not in a read-then-write in Go, so two requests racing to reclaim the same row cannot both win.

The concurrency tests are not simulations. At the repository level, up to eight goroutines race `ClaimAttempt` against a real SQLite file and exactly one may win. At the service level, four goroutines call `SyncOrder` against a deliberately slow ERP, and the assertion is on the ERP's own call counter: exactly one dispatch, with every other caller either replaying or getting a 409. Both run under `-race`.

**Tradeoff**: SQLite has a single writer, which makes this correct and simple but caps write throughput. The design ports to Postgres unchanged, since `ON CONFLICT DO NOTHING` and the conditional `UPDATE` are the same there. Row-level locking would only matter at a write volume this service is nowhere near.

## Chunking versus the idempotency key contract

The spec pins the header to `X-Idempotency-Key: SHA256(order_id + tenant_id)`. But ERP A caps a request at two line items, so a three-line order is three requests carrying **the same key**. A correctly-implemented ERP would dedupe chunks 2 and 3 as replays of chunk 1 and silently drop most of the order.

I kept the specified key exactly as written and added `X-Chunk-Index`. The mock dedupes on the pair. The spec's value is unchanged, and the chunked case is not silently broken.

If the real ERP A cannot accept a second header, the alternative is folding the chunk index into the key itself (`SHA256(order_id + tenant_id + ":" + n)`) — which breaks the letter of the spec, so it is the tenant's call, not mine.

One property of the specified key is worth flagging to the tenant: concatenating the two ids without a separator means a shifted boundary collides, so `("ord_", "1tenant_alpha")` hashes identically to `("ord_1", "tenant_alpha")`. It is unreachable with the current id formats, and changing it would stop matching the key ERP A computes on its side, so the behaviour is pinned by a test rather than silently "fixed".

## Other decisions worth naming

**Pre-flight rejections do not consume a claim.** An expired or unmappable order returns 422 and writes no `sync_attempts` row. If it did, a re-confirmed order could not be synced later under the same deterministic key. The cost is that pre-flight rejections are not in the audit table; they are logged instead. If audit coverage of rejections matters more, that flips.

**Retry only on 429 and 5xx.** A 4xx is the ERP saying the payload is wrong; repeating it just makes the order taker wait longer for the same answer. Backoff is exponential with full jitter and honours `Retry-After`; jitter keeps a batch from re-colliding on the rate limit in lockstep.

**Dispatch stops at the first undeliverable chunk.** Pushing chunk 3 when chunk 2 never landed produces an order state that is harder to reconcile than a clean stop the operator can retry.

**HTTP 207 is excluded from `Response.OK()`.** It sits in the 2xx range, so a generic success check would mark a partial order `SYNCED` — the exact mislabelling this service exists to prevent. Partial handling is opt-in via `IsMultiStatus()`. A test enforces this.

**A 207 with zero accepted lines is `FAILED`, not `PARTIAL_SUCCESS`.** Nothing reached the ERP; reporting partial would overstate it. A line the ERP omits from `line_results` counts as rejected, never as silently accepted.

**Response snapshots are a projection, not the raw struct.** `erp.Response` is not marshalled directly, because its `TransportErr` is an interface that serialises to `{}` — which would erase the failure reason at exactly the moment it matters. `responseSnapshot` records the error as a string alongside the status, attempts and SKUs.

**Messages are codes plus params, translated at the HTTP edge.** The service layer never builds a sentence, because only the handler knows the caller's `Accept-Language`. English and Malay ship (MYR, +60); adding a language is a data change to the catalog. Responses carry both the rendered `message` and the `message_code`, so a human and an automated caller are both served. A test asserts every code resolves in both languages with no leftover placeholder, so a half-translated catalog fails the build.

**Request validation is hand-written, not a validation library.** `BatchSyncRequest` has one field and three rules. A struct-tag validator would have meant a dependency plus a wrapper to get JSON field names into the error text, which is more code than the check itself.

**`Accept-Language` parsing reads the first supported tag and ignores q-weights.** A full RFC 4647 matcher is a dependency a two-language catalog does not earn. Unsupported tags fall back to English rather than leaking a raw code.

**Migrations run on boot** via golang-migrate over an embedded FS, so a local run and the test harness are always on the same schema. The seed is a migration, which is fine for an exercise and would move behind a flag in production.

**SQLite over an in-memory map.** The exercise allows either, but the concurrency story is only convincing if a real constraint enforces it. `modernc.org/sqlite` is pure Go, so there is no CGO and tests run anywhere. Tests use a temp *file* rather than `:memory:`, because an in-memory DSN hands every pooled connection its own empty database.

## What the tests caught

Recorded because none of it was visible by inspection:

- **The whole sync path was dead on a real boot.** `migrations.Run` was never wired into `cmd/`, so the service started against an empty database and every sync returned 500. Every test migrated explicitly, so the suite was green. Running the two binaries and driving them with curl is what found it, which is the argument for `make demo` existing at all.
- **`GetOrderByID` filtered on `WHERE id = ?`**, a column `orders` does not have — it is keyed on `order_id`. Both order queries also used `SELECT *`, returning columns the structs do not map, which sqlx rejects.
- **`getLines` ordered by `line_no`**, a column `sync_line_results` does not have, so every read of an attempt's per-line detail would have errored.

## What I would do next

Given more than the timebox, in priority order:

1. **A reaper for abandoned claims.** The 2-minute reclaim window is passive. A background sweep that marks long-`PENDING` rows `FAILED` with a clear message would beat waiting for someone to click again.
2. **Outbox instead of synchronous dispatch.** Today the HTTP request holds open for the duration of the ERP call. Writing an outbox row and draining it in a worker would make the endpoint fast and survive a crash mid-dispatch without the reclaim heuristic.
3. **Per-tenant rate limit config.** ERP A's limit of two is hardcoded in its adapter. It belongs in config once a second tenant has a different one.
4. **Contract tests against the real ERP sandboxes.** The mock encodes my reading of the spec. That reading is the largest untested assumption in the whole service.
5. **Metrics.** Sync outcomes by tenant and status, ERP latency, retry counts. The audit table answers "what happened to this order"; it does not answer "is ERP B degrading right now".
