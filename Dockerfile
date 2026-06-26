FROM debian:bookworm-slim AS base
RUN apt-get update && apt-get install -y ca-certificates libc6 && rm -rf /var/lib/apt/lists/*

FROM golang:1.22-bookworm AS gateway-builder
WORKDIR /app
COPY services/gateway ./services/gateway
WORKDIR /app/services/gateway
RUN go mod download || true
RUN go build -o /orchestrator .

FROM rust:1.78-bookworm AS compliance-builder
WORKDIR /app
COPY services/compliance ./services/compliance
WORKDIR /app/services/compliance
RUN cargo build --release
RUN cp target/release/compliance-daemon /compliance-daemon

# Gateway Image
FROM base AS gateway
COPY --from=gateway-builder /orchestrator /usr/local/bin/orchestrator
EXPOSE 8080
ENTRYPOINT ["orchestrator"]

# Compliance Image
FROM base AS compliance
COPY --from=compliance-builder /compliance-daemon /usr/local/bin/compliance-daemon
EXPOSE 9095
ENTRYPOINT ["compliance-daemon", "--port", "9095"]
