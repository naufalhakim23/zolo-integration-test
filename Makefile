.PHONY: start mock-erp test test-race build clean tidy demo

start:
	go run .

mock-erp:
	go run ./cmd/mockerp

test:
	go test ./...

test-race:
	go test -race -count=1 ./...

build:
	go build -o bin/zolo-integration .
	go build -o bin/mock-erp ./cmd/mockerp

tidy:
	go mod tidy

clean:
	rm -rf bin zolo.db zolo.db-shm zolo.db-wal

# demo drives every edge case against a running pair of services.
# Run `make mock-erp` and `make start` in two other terminals first.
demo:
	@echo "\n== tenant_alpha, 3 line items, chunked around ERP A's 429 =="
	@curl -s -o /dev/null -w "  HTTP %{http_code}\n" -X POST localhost:8080/api/v1/orders/ord_998123/sync
	@echo "== double-click: the same order again, replayed, not resubmitted =="
	@curl -s -X POST localhost:8080/api/v1/orders/ord_998123/sync | grep -o '"replayed":[a-z]*' || true
	@echo "\n== tenant_beta, HTTP 207 partial success =="
	@curl -s -o /dev/null -w "  HTTP %{http_code}\n" -X POST localhost:8080/api/v1/orders/ord_998200/sync
	@echo "== tenant_beta, confirmed 30 hours ago, rejected pre-flight =="
	@curl -s -o /dev/null -w "  HTTP %{http_code}\n" -X POST localhost:8080/api/v1/orders/ord_expired/sync
	@echo "== the same rejection in Malay =="
	@curl -s -H 'Accept-Language: ms' -X POST localhost:8080/api/v1/orders/ord_expired/sync
	@echo "\n== audit read =="
	@curl -s localhost:8080/api/v1/orders/ord_998200/sync-status
	@echo ""
