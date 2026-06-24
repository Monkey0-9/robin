#pragma once

#include "order_state.hpp"
#include "matching_engine.hpp"
#include "risk_checks.hpp"
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <cstdlib>
#include <string>
#include <thread>
#include <atomic>
#include <chrono>

#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
#pragma comment(lib, "ws2_32.lib")
#else
#include <sys/socket.h>
#include <netinet/in.h>
#include <unistd.h>
#include <arpa/inet.h>
#include <fcntl.h>
#endif

namespace quantum {
namespace execution {

#ifdef _WIN32
typedef SOCKET socket_t;
#else
typedef int socket_t;
#endif

struct FillResult {
    uint64_t order_id;
    uint32_t instrument_id;
    uint32_t fill_price;
    uint32_t fill_qty;
    OrderState state;
    bool success;
};

class OrderServer {
public:
    OrderServer(MatchingEngine* engine, uint16_t port)
        : engine_(engine), port_(port), running_(false), sock_(-1)
    {}

    ~OrderServer() { stop(); }

    bool start() {
#ifdef _WIN32
        WSADATA wsa;
        if (WSAStartup(MAKEWORD(2,2), &wsa) != 0) return false;
#endif
        sock_ = (socket_t)::socket(AF_INET, SOCK_STREAM, 0);
#ifdef _WIN32
        if (sock_ == INVALID_SOCKET) return false;
#else
        if (sock_ < 0) return false;
#endif
        int opt = 1;
        ::setsockopt(sock_, SOL_SOCKET, SO_REUSEADDR, (const char*)&opt, sizeof(opt));

        sockaddr_in addr{};
        addr.sin_family = AF_INET;
        addr.sin_addr.s_addr = INADDR_ANY;
        addr.sin_port = htons(port_);

        if (::bind(sock_, (sockaddr*)&addr, sizeof(addr)) < 0) return false;
        if (::listen(sock_, 16) < 0) return false;

        running_ = true;
        thread_ = std::thread(&OrderServer::accept_loop, this);
        return true;
    }

    void stop() {
        running_ = false;
#ifdef _WIN32
        if (sock_ != INVALID_SOCKET && sock_ != (socket_t)-1) { ::closesocket(sock_); WSACleanup(); }
#else
        if (sock_ >= 0) { ::close(sock_); }
#endif
        sock_ = -1;
        if (thread_.joinable()) thread_.join();
    }

    RiskEngine& risk() { return risk_; }

private:
    void accept_loop() {
        while (running_) {
            sockaddr_in client;
#ifdef _WIN32
            int client_len = sizeof(client);
            socket_t client_sock = ::accept(sock_, (sockaddr*)&client, &client_len);
            if (client_sock == INVALID_SOCKET) continue;
#else
            socklen_t client_len = sizeof(client);
            socket_t client_sock = ::accept(sock_, (sockaddr*)&client, &client_len);
            if (client_sock < 0) continue;
#endif
            std::thread(&OrderServer::handle_client, this, client_sock).detach();
        }
    }

    void handle_client(socket_t client_sock) {
        char buf[4096];
#ifdef _WIN32
        int n = ::recv(client_sock, buf, sizeof(buf) - 1, 0);
#else
        int n = (int)::recv(client_sock, buf, sizeof(buf) - 1, 0);
#endif
        if (n <= 0) {
#ifdef _WIN32
            ::closesocket(client_sock);
#else
            ::close(client_sock);
#endif
            return;
        }
        buf[n] = '\0';
        std::string request(buf);
        while (!request.empty() && (request.back() == '\n' || request.back() == '\r' || request.back() == ' '))
            request.pop_back();

        if (request == "health") {
            const char* resp = "{\"status\":\"ok\"}\n";
            ::send(client_sock, resp, (int)std::strlen(resp), 0);
        } else if (request.find("order") != std::string::npos || request.find("id") != std::string::npos) {
            FillResult fill = process_order_json(request);
            char resp[512];
            int len = std::snprintf(resp, sizeof(resp),
                "{\"order_id\":%llu,\"instrument_id\":%u,\"fill_price\":%u,\"fill_qty\":%u,\"status\":\"%s\",\"success\":%s}\n",
                (unsigned long long)fill.order_id, fill.instrument_id, fill.fill_price, fill.fill_qty,
                fill.state == OrderState::FILLED ? "FILLED" :
                fill.state == OrderState::WORKING ? "WORKING" :
                fill.state == OrderState::CANCELED ? "CANCELED" :
                fill.state == OrderState::REJECTED ? "REJECTED" : "UNKNOWN",
                fill.success ? "true" : "false");
            ::send(client_sock, resp, len, 0);
        } else {
            const char* resp = "{\"error\":\"unknown command\"}\n";
            ::send(client_sock, resp, (int)std::strlen(resp), 0);
        }

#ifdef _WIN32
        ::closesocket(client_sock);
#else
        ::close(client_sock);
#endif
    }

    FillResult process_order_json(const std::string& json) {
        FillResult result{};
        result.success = false;
        result.state = OrderState::REJECTED;

        uint64_t id = 0, instrument_id = 1;
        uint32_t price = 0, qty = 0;
        Side side = Side::BID;
        OrderType type = OrderType::LIMIT;
        uint64_t timestamp = 0;

        auto extract_uint64 = [&](const std::string& key, uint64_t& val) -> bool {
            auto pos = json.find("\"" + key + "\"");
            if (pos == std::string::npos) return false;
            auto sep = json.find(':', pos);
            if (sep == std::string::npos) return false;
            sep++;
            while (sep < json.size() && (json[sep] == ' ' || json[sep] == '\t')) sep++;
            char* end = nullptr;
            val = std::strtoull(json.c_str() + sep, &end, 10);
            return true;
        };

        auto extract_str = [&](const std::string& key, std::string& val) -> bool {
            auto pos = json.find("\"" + key + "\"");
            if (pos == std::string::npos) return false;
            auto sep = json.find(':', pos);
            if (sep == std::string::npos) return false;
            sep++;
            while (sep < json.size() && (json[sep] == ' ' || json[sep] == '\t')) sep++;
            if (json[sep] == '"') {
                sep++;
                while (sep < json.size() && json[sep] != '"') val += json[sep++];
                return true;
            }
            return false;
        };

        uint64_t temp;
        extract_uint64("id", id);
        if (extract_uint64("instrument_id", temp)) instrument_id = temp;
        if (extract_uint64("price", temp)) price = (uint32_t)temp;
        if (extract_uint64("qty", temp)) qty = (uint32_t)temp;
        if (extract_uint64("timestamp", temp)) timestamp = temp;
        std::string side_str, type_str;
        extract_str("side", side_str);
        extract_str("type", type_str);

        if (side_str == "ASK" || side_str == "SELL" || side_str == "ask" || side_str == "sell")
            side = Side::ASK;
        if (type_str == "MARKET" || type_str == "market")
            type = OrderType::MARKET;
        else if (type_str == "IOC" || type_str == "ioc")
            type = OrderType::IOC;
        else if (type_str == "FOK" || type_str == "fok")
            type = OrderType::FOK;

        if (id == 0) id = static_cast<uint64_t>(std::time(nullptr)) * 1000000ULL;
        if (timestamp == 0) timestamp = static_cast<uint64_t>(
            std::chrono::duration_cast<std::chrono::nanoseconds>(
                std::chrono::steady_clock::now().time_since_epoch()).count());

        Order order;
        std::memset(&order, 0, sizeof(order));
        order.id = id;
        order.price = price;
        order.qty = qty;
        order.instrument_id = static_cast<uint32_t>(instrument_id);
        order.side = side;
        order.type = type;
        order.state = OrderState::NEW;

        if (!risk_.check_order(order, timestamp)) {
            result.order_id = id;
            result.instrument_id = order.instrument_id;
            result.state = OrderState::REJECTED;
            return result;
        }

        if (!engine_->submit_order(order)) {
            risk_.rollback_position(order);
            result.state = OrderState::REJECTED;
            return result;
        }

        result.order_id = id;
        result.instrument_id = order.instrument_id;
        result.success = true;

        Trade trade;
        // Poll for trade result with timeout (matching engine processes async)
        for (int poll = 0; poll < 50; poll++) {
            if (engine_->poll_trade(trade)) {
                result.fill_price = trade.price;
                result.fill_qty = trade.qty;
                result.state = OrderState::FILLED;
                risk_.update_reference_price(order.instrument_id, trade.price);
                return result;
            }
            std::this_thread::sleep_for(std::chrono::microseconds(100));
        }

        // Also check if another pending order might fill this in the future
        result.state = OrderState::WORKING;
        return result;
    }

    MatchingEngine* engine_;
    uint16_t port_;
    std::atomic<bool> running_;
    std::thread thread_;
    socket_t sock_;
    RiskEngine risk_;
};

}} // namespace
