#include "fix_gateway.hpp"
#include <iostream>

namespace quantum {
namespace execution {

void FIXGateway::onCreate(const FIX::SessionID& sessionID) {
    std::cout << "[FIX] Session created: " << sessionID.toString() << std::endl;
}

void FIXGateway::onLogon(const FIX::SessionID& sessionID) {
    std::cout << "[FIX] Logon: " << sessionID.toString() << std::endl;
}

void FIXGateway::onLogout(const FIX::SessionID& sessionID) {
    std::cout << "[FIX] Logout: " << sessionID.toString() << std::endl;
}

void FIXGateway::toAdmin(FIX::Message& message, const FIX::SessionID& sessionID) {
    // Inject credentials if required
}

void FIXGateway::toApp(FIX::Message& message, const FIX::SessionID& sessionID) throw(FIX::DoNotSend) {
    // Outbound application message (e.g. ExecutionReport)
}

void FIXGateway::fromAdmin(const FIX::Message& message, const FIX::SessionID& sessionID)
    throw(FIX::FieldNotFound, FIX::IncorrectDataFormat, FIX::IncorrectTagValue, FIX::RejectLogon) {
}

void FIXGateway::fromApp(const FIX::Message& message, const FIX::SessionID& sessionID)
    throw(FIX::FieldNotFound, FIX::IncorrectDataFormat, FIX::IncorrectTagValue, FIX::UnsupportedMessageType) {
    
    // Crack the message to specific type handlers
    crack(message, sessionID);
}

void FIXGateway::onMessage(const FIX50SP2::NewOrderSingle& message, const FIX::SessionID& sessionID) {
    std::cout << "[FIX] Received NewOrderSingle from " << sessionID.toString() << std::endl;
    
    // Extract FIX fields
    FIX::ClOrdID clOrdID;
    FIX::Symbol symbol;
    FIX::Side side;
    FIX::OrderQty orderQty;
    FIX::Price price;

    message.get(clOrdID);
    message.get(symbol);
    message.get(side);
    message.get(orderQty);
    
    if (message.isSetField(FIX::FIELD::Price)) {
        message.get(price);
    }

    // Pass to internal matcher or router
    // This connects to the SHM ring buffer natively
}

} // namespace execution
} // namespace quantum
