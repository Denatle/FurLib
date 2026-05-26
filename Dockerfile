FROM --platform=$BUILDPLATFORM golang:1.26.3-alpine AS builder
LABEL authors="denatle"

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG TARGETOS

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS \
    go build -ldflags="-s -w" -o furlib ./cmd/app

FROM alpine:3.19

RUN apk add --no-cache sqlite-libs ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/furlib .

VOLUME ["/app/data"]

EXPOSE 8080

CMD ["./furlib"]
