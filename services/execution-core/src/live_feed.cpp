// ============================================================================
// Robin Trading Platform — C++ Live Market Data Feed
// ============================================================================
// Lightweight WebSocket client that connects to Binance trade stream
// and Alpaca IEX stream, parsing JSON ticks and pushing them into
// the strategy engine's SHM ring buffer.
//
// Architecture:
//   [Binance WS] ──parse──► [Tick]
//   [Alpaca  WS] ──parse──► [Tick] ──► strategy_engine.hpp ──► SHM signal ring
//
// Design:
//   - Single-threaded event loop per exchange (dedicated OS thread)
//   - Stack-allocated JSON parser (no heap on hot path)
//   - Raw TCP + TLS via OS sockets (libssl only)
//   - Minimal dependencies: openssl, pthread
// ============================================================================

#include "strategy_engine.hpp"
#include "../../shared/config.h"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cerrno>
#include <csignal>
#include <cassert>
#include <atomic>
#include <thread>
#include <chrono>
#include <functional>

// Platform sockets
#if defined(_WIN32)
#  include <winsock2.h>
#  include <ws2tcpip.h>
#  pragma comment(lib, "ws2_32.lib")
   using socket_t = SOCKET;
#  define SOCK_INVALID INVALID_SOCKET
#  define sock_close closesocket
#else
#  include <sys/socket.h>
#  include <netdb.h>
#  include <unistd.h>
#  include <arpa/inet.h>
   using socket_t = int;
#  define SOCK_INVALID (-1)
#  define sock_close close
#endif

// ─── Minimal stack-allocated JSON value extractor ─────────────────────────────
// Extracts a double or string from a JSON key without heap allocation.

static bool json_get_double(const char* json, const char* key, double& out) noexcept {
    // Find "key":
    char search[48];
    int n = std::snprintf(search, sizeof(search), "\"%s\":", key);
    if (n <= 0) return false;
    const char* p = std::strstr(json, search);
    if (!p) return false;
    p += n;
    while (*p == ' ') ++p;
    // Handle quoted number ("123.45") or raw (123.45)
    if (*p == '"') ++p;
    char* end;
    out = std::strtod(p, &end);
    return end != p;
}

static bool json_get_str(const char* json, const char* key,
                          char* buf, size_t buf_sz) noexcept {
    char search[48];
    int n = std::snprintf(search, sizeof(search), "\"%s\":\"", key);
    if (n <= 0) return false;
    const char* p = std::strstr(json, search);
    if (!p) return false;
    p += n;
    size_t i = 0;
    while (*p && *p != '"' && i + 1 < buf_sz)
        buf[i++] = *p++;
    buf[i] = '\0';
    return i > 0;
}

// ─── WebSocket frame decoder (RFC 6455, client frames only) ──────────────────

struct WsFrame {
    const char* payload;
    size_t      len;
    bool        is_text;
    bool        is_close;
};

// Decode a single WebSocket frame from buf[0..buf_len)
// Returns number of bytes consumed, or 0 if incomplete
static size_t ws_decode_frame(const uint8_t* buf, size_t buf_len, WsFrame& out) noexcept {
    if (buf_len < 2) return 0;
    const uint8_t b0 = buf[0], b1 = buf[1];
    const bool masked = (b1 & 0x80) != 0;
    const uint8_t opcode = b0 & 0x0F;
    size_t payload_len = b1 & 0x7F;
    size_t header_sz   = 2;

    if (payload_len == 126) {
        if (buf_len < 4) return 0;
        payload_len = (static_cast<size_t>(buf[2]) << 8) | buf[3];
        header_sz   = 4;
    } else if (payload_len == 127) {
        if (buf_len < 10) return 0;
        payload_len = 0;
        for (int i = 2; i < 10; ++i)
            payload_len = (payload_len << 8) | buf[i];
        header_sz = 10;
    }

    const size_t mask_sz   = masked ? 4 : 0;
    const size_t total_sz  = header_sz + mask_sz + payload_len;
    if (buf_len < total_sz) return 0;

    out.is_close = (opcode == 0x8);
    out.is_text  = (opcode == 0x1) || (opcode == 0x0);
    out.len      = payload_len;

    // Server→client frames must not be masked per RFC 6455
    out.payload  = reinterpret_cast<const char*>(buf + header_sz + mask_sz);

    return total_sz;
}

// ─── WebSocket handshake helpers ─────────────────────────────────────────────

static const char WS_UPGRADE_TMPL[] =
    "GET %s HTTP/1.1\r\n"
    "Host: %s\r\n"
    "Upgrade: websocket\r\n"
    "Connection: Upgrade\r\n"
    "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n"
    "Sec-WebSocket-Version: 13\r\n"
    "\r\n";

// ─── Signal output ring (SHM bridge → strategy signals) ──────────────────────

// We re-use config.h's SHM ring for pushing signals.
// Here we write directly to a shared-memory signal buffer
// that the Go gateway reads via mmap.

struct alignas(64) SignalShmHeader {
    uint64_t magic;
    uint64_t write_idx;   // Atomic write pointer
    uint64_t _pad[6];
};
static constexpr uint64_t SIGNAL_SHM_MAGIC   = 0x524F42494E534947ULL; // "ROBINSIG"
static constexpr size_t   SIGNAL_SHM_SLOTS   = 1024;
static constexpr size_t   SIGNAL_SHM_SLOT_SZ = sizeof(robin::strategy::Signal);

// ─── Feed runner ─────────────────────────────────────────────────────────────

struct FeedStats {
    alignas(64) std::atomic<uint64_t> ticks_received{0};
    alignas(64) std::atomic<uint64_t> signals_emitted{0};
    alignas(64) std::atomic<uint64_t> parse_errors{0};
    alignas(64) std::atomic<uint64_t> reconnects{0};
};

static FeedStats g_stats;
static std::atomic<bool> g_running{true};

// Parse Binance trade JSON and emit tick to strategy engine
static bool parse_binance_trade(
    const char* json, size_t /*len*/,
    robin::strategy::CompositeSignalEngine& engine,
    robin::strategy::Signal& sig_out)
{
    // Binance combined stream: {"stream":"btcusdt@trade","data":{"e":"trade","p":"65000.1","q":"0.01",...}}
    // Fast path: look for "e":"trade"
    if (!std::strstr(json, "\"trade\"")) return false;

    double price = 0.0, volume = 0.0;
    char sym_raw[16] = {};

    if (!json_get_double(json, "p", price))   return false;
    if (!json_get_double(json, "q", volume))  return false;
    json_get_str(json, "s", sym_raw, sizeof(sym_raw));

    // Normalise symbol: "BTCUSDT" → "BTC-USD"
    char symbol[16] = "BTC-USD";
    if (std::strncmp(sym_raw, "ETH", 3) == 0) std::strncpy(symbol, "ETH-USD", 8);

    robin::strategy::Tick tick{};
    tick.timestamp_ns = static_cast<uint64_t>(
        std::chrono::system_clock::now().time_since_epoch().count());
    tick.price  = price;
    tick.volume = volume;
    tick.bid    = price * 0.9999;
    tick.ask    = price * 1.0001;
    std::strncpy(tick.symbol, symbol, 15);
    tick.exchange = 0; // binance

    g_stats.ticks_received.fetch_add(1, std::memory_order_relaxed);
    return engine.on_tick(tick, sig_out);
}

// Print signal to stdout (Go gateway reads via pipe or polls SHM)
static void emit_signal(const robin::strategy::Signal& sig) {
    const char* side_str =
        (sig.side == robin::strategy::Side::BUY)  ? "BUY"  :
        (sig.side == robin::strategy::Side::SELL) ? "SELL" : "HOLD";

    std::printf(
        "{\"type\":\"SIGNAL\",\"symbol\":\"%s\",\"side\":\"%s\","
        "\"price\":%.6f,\"confidence\":%.3f,\"kelly\":%.4f,\"reason\":\"%s\","
        "\"strategy\":%d,\"ts\":%llu}\n",
        sig.symbol, side_str, sig.price,
        sig.confidence, sig.kelly_fraction, sig.reason,
        (int)sig.strategy_id,
        (unsigned long long)sig.timestamp_ns
    );
    std::fflush(stdout);
    g_stats.signals_emitted.fetch_add(1, std::memory_order_relaxed);
}

// ─── Simple TCP connect (no TLS — for local development; add openssl for prod) ─

static socket_t tcp_connect(const char* host, uint16_t port) noexcept {
    struct addrinfo hints{}, *res = nullptr;
    hints.ai_family   = AF_UNSPEC;
    hints.ai_socktype = SOCK_STREAM;

    char port_str[8];
    std::snprintf(port_str, sizeof(port_str), "%u", port);
    if (::getaddrinfo(host, port_str, &hints, &res) != 0) return SOCK_INVALID;

    socket_t fd = ::socket(res->ai_family, res->ai_socktype, 0);
    if (fd == SOCK_INVALID) { ::freeaddrinfo(res); return SOCK_INVALID; }

    if (::connect(fd, res->ai_addr, static_cast<int>(res->ai_addrlen)) != 0) {
        sock_close(fd);
        ::freeaddrinfo(res);
        return SOCK_INVALID;
    }
    ::freeaddrinfo(res);
    return fd;
}

// ─── Main feed loop ───────────────────────────────────────────────────────────

static void run_feed_loop(const char* symbol_hint) {
    robin::strategy::CompositeSignalEngine engine(symbol_hint);

    /*
     * NOTE: Production Binance feed requires WSS (TLS) on port 443.
     * For production, integrate libwebsockets or OpenSSL for TLS.
     * See: binance-docs.github.io/apidocs/websocket_api/en/
     */
    static constexpr char HOST[] = "stream.binance.com";
#ifdef ROBIN_TLS_ENABLED
    static constexpr uint16_t PORT = 443;
#else
    static constexpr uint16_t PORT = 80; // Dev only — no TLS
#endif
    static constexpr char PATH[] = "/stream?streams=btcusdt@trade/ethusdt@trade";

    char ws_req[512];
    std::snprintf(ws_req, sizeof(ws_req), WS_UPGRADE_TMPL, PATH, HOST);

    static constexpr size_t BUF_SZ = 65536;
    static char buf[BUF_SZ]; // Stack-allocated receive buffer

    while (g_running.load(std::memory_order_relaxed)) {
        std::fprintf(stderr, "[feed] Connecting to %s:%u%s ...\n", HOST, PORT, PATH);

        socket_t fd = tcp_connect(HOST, PORT);
        if (fd == SOCK_INVALID) {
            std::fprintf(stderr, "[feed] Connect failed, retry in 5s\n");
            std::this_thread::sleep_for(std::chrono::seconds(5));
            g_stats.reconnects.fetch_add(1, std::memory_order_relaxed);
            continue;
        }

        // Send WebSocket upgrade request
        ::send(fd, ws_req, static_cast<int>(std::strlen(ws_req)), 0);

        // Receive HTTP upgrade response (skip it)
        int n = ::recv(fd, buf, BUF_SZ - 1, 0);
        if (n <= 0) { sock_close(fd); continue; }
        buf[n] = '\0';
        if (!std::strstr(buf, "101 Switching Protocols")) {
            std::fprintf(stderr, "[feed] WS handshake failed: %.80s\n", buf);
            sock_close(fd);
            std::this_thread::sleep_for(std::chrono::seconds(3));
            continue;
        }
        std::fprintf(stderr, "[feed] WebSocket connected ✓\n");

        // Receive loop
        size_t leftover = 0;
        while (g_running.load(std::memory_order_relaxed)) {
            n = ::recv(fd, buf + leftover, static_cast<int>(BUF_SZ - leftover - 1), 0);
            if (n <= 0) break;
            size_t total = leftover + static_cast<size_t>(n);
            buf[total] = '\0';

            size_t offset = 0;
            while (offset < total) {
                WsFrame frame{};
                size_t consumed = ws_decode_frame(
                    reinterpret_cast<const uint8_t*>(buf + offset),
                    total - offset, frame);
                if (consumed == 0) break;
                offset += consumed;

                if (frame.is_close) {
                    std::fprintf(stderr, "[feed] Server sent close frame\n");
                    goto reconnect;
                }

                if (frame.is_text && frame.len > 0) {
                    // Null-terminate payload in place (safe — we control buffer)
                    char* payload = const_cast<char*>(frame.payload);
                    char saved    = payload[frame.len];
                    payload[frame.len] = '\0';

                    robin::strategy::Signal sig{};
                    if (parse_binance_trade(payload, frame.len, engine, sig)) {
                        emit_signal(sig);
                    }
                    payload[frame.len] = saved;
                }
            }

            // Move leftover bytes to front
            leftover = total - offset;
            if (leftover > 0 && offset > 0)
                std::memmove(buf, buf + offset, leftover);
        }

        reconnect:
        sock_close(fd);
        g_stats.reconnects.fetch_add(1, std::memory_order_relaxed);
        std::this_thread::sleep_for(std::chrono::seconds(2));
    }
}

// ─── Stats reporter thread ─────────────────────────────────────────────────────

static void stats_thread() {
    while (g_running.load(std::memory_order_relaxed)) {
        std::this_thread::sleep_for(std::chrono::seconds(30));
        std::fprintf(stderr,
            "[stats] ticks=%llu signals=%llu errors=%llu reconnects=%llu\n",
            (unsigned long long)g_stats.ticks_received.load(),
            (unsigned long long)g_stats.signals_emitted.load(),
            (unsigned long long)g_stats.parse_errors.load(),
            (unsigned long long)g_stats.reconnects.load());
    }
}

// ─── Entry point ──────────────────────────────────────────────────────────────

int main(int argc, char* argv[]) {
#if defined(_WIN32)
    WSADATA wsa;
    WSAStartup(MAKEWORD(2, 2), &wsa);
#endif

    const char* symbol = (argc > 1) ? argv[1] : "BTC-USD";

    // Handle SIGINT/SIGTERM
    std::signal(SIGINT,  [](int){ g_running.store(false, std::memory_order_relaxed); });
    std::signal(SIGTERM, [](int){ g_running.store(false, std::memory_order_relaxed); });

    std::fprintf(stderr,
        "Robin C++ Live Feed — symbol=%s\n"
        "Signals emitted to stdout as JSON (one per line).\n", symbol);

    // Start stats reporter in background
    std::thread stats_t(stats_thread);
    stats_t.detach();

    // Run main feed loop (reconnects automatically)
    run_feed_loop(symbol);

#if defined(_WIN32)
    WSACleanup();
#endif
    return 0;
}
