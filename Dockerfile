# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && \
    addgroup -S dicebet && adduser -S dicebet -G dicebet
USER dicebet

COPY --from=builder /out/server /usr/local/bin/server
COPY --from=builder /out/migrate /usr/local/bin/migrate

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
