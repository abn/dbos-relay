# Build stage
FROM golang:1.26-alpine AS builder

ENV GOTOOLCHAIN=local

WORKDIR /src

# Download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build static binary
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X github.com/abn/relay/internal/cli.Version=${VERSION}" -o /bin/relay ./cmd/relay

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

USER 65532:65532

COPY --from=builder /bin/relay /bin/relay
COPY LICENSE NOTICE THIRD-PARTY-LICENSES.md /

EXPOSE 8090

ENTRYPOINT ["/bin/relay"]
CMD ["version"]
