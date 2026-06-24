#include "fix_gateway.hpp"
#include <fstream>
#include <sstream>
#include <iostream>
#include <cstdlib>

namespace quantum {
namespace execution {

FIXGateway::FIXGateway()
    : initialized_(false) {}

bool FIXGateway::init(const std::string& configPath) {
    configPath_ = configPath;

    try {
        // Load session settings from a .cfg file
        settings_ = std::make_unique<FIX::SessionSettings>(configPath);
    } catch (const std::exception& e) {
        // If no config file exists, generate default settings inline
        std::cerr << "[FIX] No config file at " << configPath
                  << " (" << e.what() << "). Using default settings." << std::endl;

        // Generate settings from env vars or defaults
        std::string senderCompID = std::getenv("FIX_SENDER_COMP_ID") ? std::getenv("FIX_SENDER_COMP_ID") : "ROBIN";
        std::string targetCompID = std::getenv("FIX_TARGET_COMP_ID") ? std::getenv("FIX_TARGET_COMP_ID") : "BROKER";
        std::string host = std::getenv("FIX_BROKER_HOST") ? std::getenv("FIX_BROKER_HOST") : "127.0.0.1";
        std::string port = std::getenv("FIX_BROKER_PORT") ? std::getenv("FIX_BROKER_PORT") : "4198";

        std::stringstream ss;
        ss << "[DEFAULT]" << std::endl
           << "ConnectionType=initiator" << std::endl
           << "ReconnectInterval=5" << std::endl
           << "FileStorePath=store" << std::endl
           << "FileLogPath=logs" << std::endl
           << "StartTime=00:00:00" << std::endl
           << "EndTime=00:00:00" << std::endl
           << "HeartBtInt=30" << std::endl
           << "SocketConnectPort=" << port << std::endl
           << "SocketConnectHost=" << host << std::endl
           << "BeginString=FIX.4.4" << std::endl
           << "DefaultApplVerID=FIX50SP2" << std::endl
           << "ResetOnLogon=Y" << std::endl
           << "ResetOnLogout=Y" << std::endl
           << "ResetOnDisconnect=Y" << std::endl
           << "CheckLatency=N" << std::endl
           << std::endl
           << "[SESSION]" << std::endl
           << "BeginString=FIX.4.4" << std::endl
           << "SenderCompID=" << senderCompID << std::endl
           << "TargetCompID=" << targetCompID << std::endl
           << "HeartBtInt=30" << std::endl
           << "SocketConnectPort=" << port << std::endl
           << "SocketConnectHost=" << host << std::endl;

        settings_ = std::make_unique<FIX::SessionSettings>(ss);
    }

    storeFactory_ = std::make_unique<FIX::FileStoreFactory>(*settings_);
    logFactory_ = std::make_unique<FIX::ScreenLogFactory>(*settings_);
    initiator_ = std::make_unique<FIX::SocketInitiator>(*this, *storeFactory_, *settings_, *logFactory_);

    initiator_->start();
    initialized_ = true;
    std::cout << "[FIX] Gateway initialized with " << settings_->getSessions().size() << " session(s)" << std::endl;
    return true;
}

bool FIXGateway::sendExecutionReport(
    const std::string& clOrdID,
    const std::string& orderID,
    const std::string& execID,
    char execType,
    char ordStatus,
    const std::string& symbol,
    char side,
    double lastQty,
    double lastPx,
    double leavesQty)
{
    FIX50SP2::ExecutionReport exec;
    exec.set(FIX::ClOrdID(clOrdID));
    exec.set(FIX::OrderID(orderID));
    exec.set(FIX::ExecID(execID));
    exec.setField(FIX::ExecType(execType));
    exec.setField(FIX::OrdStatus(ordStatus));
    exec.set(FIX::Symbol(symbol));
    exec.setField(FIX::Side(side));
    exec.set(FIX::LastQty(lastQty));
    exec.set(FIX::LastPx(lastPx));
    exec.set(FIX::LeavesQty(leavesQty));
    exec.set(FIX::CumQty(lastQty));
    exec.set(FIX::TransactTime());

    try {
        FIX::Session::sendToTarget(exec);
        return true;
    } catch (const std::exception& e) {
        std::cerr << "[FIX] Failed to send ExecutionReport: " << e.what() << std::endl;
        return false;
    }
}

void FIXGateway::onNewOrderSingle(NewOrderSingleCallback cb) {
    newOrderCb_ = cb;
}

void FIXGateway::stop() {
    if (initiator_) {
        initiator_->stop();
    }
    initialized_ = false;
    std::cout << "[FIX] Gateway stopped" << std::endl;
}

// ============================================================================
// FIX::Application interface
// ============================================================================

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
    if (FIX::Header header = message.getHeader(); header.getField(FIX::FIELD::MsgType) == "A") {
        std::cout << "[FIX] Sending Logon for " << sessionID.toString() << std::endl;
    }
}

void FIXGateway::toApp(FIX::Message& message, const FIX::SessionID& sessionID) throw(FIX::DoNotSend) {
    // Outbound application message validation
}

void FIXGateway::fromAdmin(const FIX::Message& message, const FIX::SessionID& sessionID)
    throw(FIX::FieldNotFound, FIX::IncorrectDataFormat, FIX::IncorrectTagValue, FIX::RejectLogon) {
}

void FIXGateway::fromApp(const FIX::Message& message, const FIX::SessionID& sessionID)
    throw(FIX::FieldNotFound, FIX::IncorrectDataFormat, FIX::IncorrectTagValue, FIX::UnsupportedMessageType) {
    crack(message, sessionID);
}

// ============================================================================
// Message handlers
// ============================================================================

void FIXGateway::onMessage(const FIX50SP2::NewOrderSingle& message, const FIX::SessionID& sessionID) {
    std::cout << "[FIX] Received NewOrderSingle from " << sessionID.toString() << std::endl;

    FIX::ClOrdID clOrdID;
    FIX::Symbol symbol;
    FIX::Side side;
    FIX::OrderQty orderQty;
    FIX::Price price;
    FIX::OrdType ordType;
    FIX::TimeInForce timeInForce;

    message.get(clOrdID);
    message.get(symbol);
    message.get(side);
    message.get(orderQty);
    message.get(ordType);

    double priceVal = 0.0;
    if (message.isSetField(FIX::FIELD::Price)) {
        message.get(price);
        priceVal = price.getValue();
    }

    std::string tif = "0"; // Day
    if (message.isSetField(FIX::FIELD::TimeInForce)) {
        message.get(timeInForce);
        tif = std::string(1, timeInForce.getValue());
    }

    std::cout << "[FIX]   ClOrdID=" << clOrdID.getValue()
              << " Symbol=" << symbol.getValue()
              << " Side=" << side.getValue()
              << " Qty=" << orderQty.getValue()
              << " Price=" << priceVal
              << " OrdType=" << ordType.getValue()
              << " TIF=" << tif << std::endl;

    // Forward to the registered callback (routes to the matching engine via SHM)
    if (newOrderCb_) {
        newOrderCb_(
            clOrdID.getValue(),
            symbol.getValue(),
            side.getValue(),
            orderQty.getValue(),
            priceVal,
            std::string(1, ordType.getValue()),
            tif
        );
    }

    // Send back an acknowledgment ExecutionReport
    sendExecutionReport(
        clOrdID.getValue(),
        "ORD-" + clOrdID.getValue(),  // OrderID
        "EXEC-" + clOrdID.getValue(), // ExecID
        '0',   // ExecType: New
        '0',   // OrdStatus: New
        symbol.getValue(),
        side.getValue(),
        0.0,  // LastQty
        0.0,  // LastPx
        orderQty.getValue() // LeavesQty
    );
}

void FIXGateway::onMessage(const FIX50SP2::OrderCancelRequest& message, const FIX::SessionID& sessionID) {
    std::cout << "[FIX] Received OrderCancelRequest from " << sessionID.toString() << std::endl;

    FIX::ClOrdID clOrdID;
    FIX::Symbol symbol;
    FIX::Side side;

    message.get(clOrdID);
    message.get(symbol);
    message.get(side);

    // Acknowledge the cancel
    sendExecutionReport(
        clOrdID.getValue(),
        "ORD-" + clOrdID.getValue(),
        "EXEC-CXL-" + clOrdID.getValue(),
        'C',   // ExecType: Canceled
        '4',   // OrdStatus: Canceled
        symbol.getValue(),
        side.getValue(),
        0.0, 0.0, 0.0
    );
}

void FIXGateway::onMessage(const FIX50SP2::OrderCancelReplaceRequest& message, const FIX::SessionID& sessionID) {
    std::cout << "[FIX] Received OrderCancelReplaceRequest from " << sessionID.toString() << std::endl;

    FIX::ClOrdID clOrdID;
    FIX::Symbol symbol;
    FIX::Side side;
    FIX::OrderQty orderQty;

    message.get(clOrdID);
    message.get(symbol);
    message.get(side);
    message.get(orderQty);

    // Acknowledge the replace
    sendExecutionReport(
        clOrdID.getValue(),
        "ORD-" + clOrdID.getValue(),
        "EXEC-RPL-" + clOrdID.getValue(),
        'E',   // ExecType: Replace
        '5',   // OrdStatus: Replaced
        symbol.getValue(),
        side.getValue(),
        0.0, 0.0,
        orderQty.getValue()
    );
}

} // namespace execution
} // namespace quantum
