FROM gcc:13-bookworm AS builder
WORKDIR /app
RUN apt-get update && apt-get install -y cmake && rm -rf /var/lib/apt/lists/*
COPY CMakeLists.txt .
COPY src/ src/
RUN mkdir -p build && cd build && cmake .. -DCMAKE_BUILD_TYPE=Release && make -j$(nproc)

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y libstdc++6 && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/build/matching_engine /matching_engine
EXPOSE 9090
CMD ["/matching_engine"]
