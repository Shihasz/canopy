# --- Stage 1: build ---
FROM golang:1.26-bookworm AS build
WORKDIR /src

# Copy dependency manifests first so `go mod download` is cached in its
# own Docker layer — it only re-runs when go.mod/go.sum actually change,
# not on every source edit.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/canopy ./cmd/canopy

# --- Stage 2: runtime ---
# distroless/static contains no shell, no package manager, and no libc —
# appropriate here since CGO_ENABLED=0 produces a fully static binary
# with no dynamic library dependencies to satisfy.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/canopy /usr/local/bin/canopy

# distroless's "nonroot" variant already runs as an unprivileged user by
# default — no explicit USER directive needed.

ENTRYPOINT ["/usr/local/bin/canopy"]
CMD ["--help"]
