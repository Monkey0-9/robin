FROM rust:1.77-slim-bookworm AS builder
RUN apt-get update && apt-get install -y pkg-config libssl-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY Cargo.toml Cargo.lock ./
COPY risk-analytics/ risk-analytics/
COPY compliance/ compliance/

WORKDIR /app/compliance
RUN cargo build --release --bin compliance-daemon

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y libstdc++6 && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/compliance/target/release/compliance-daemon /compliance-daemon
EXPOSE 9095
CMD ["/compliance-daemon"]
