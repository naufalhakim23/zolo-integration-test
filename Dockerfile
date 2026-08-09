FROM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO stays off: modernc.org/sqlite is pure Go, so the binary is static and the runtime
# image needs no SQLite library.
RUN CGO_ENABLED=0 GOOS=linux go build -o zolo-integration . \
 && CGO_ENABLED=0 GOOS=linux go build -o mock-erp ./cmd/mockerp

FROM alpine:latest

WORKDIR /usr/local/bin

RUN apk --no-cache add ca-certificates

COPY --from=builder /build/zolo-integration .
COPY --from=builder /build/mock-erp .

CMD ["./zolo-integration"]
