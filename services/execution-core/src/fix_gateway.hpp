#pragma once
#include <quickfix/Application.h>
#include <quickfix/MessageCracker.h>
#include <quickfix/Session.h>
#include <quickfix/SessionSettings.h>
#include <quickfix/FileStore.h>
#include <quickfix/SocketInitiator.h>
#include <quickfix/fix50sp2/NewOrderSingle.h>
#include <quickfix/fix50sp2/ExecutionReport.h>
#include <quickfix/fix50sp2/OrderCancelRequest.h>
#include <quickfix/fix50sp2/OrderCancelReplaceRequest.h>
#include <string>
#include <functional>
#include <cstdint>

namespace quantum {
namespace execution {

/// Callback for incoming NewOrderSingle messages
using NewOrderSingleCallback = std::function<void(
    const std::string& clOrdID,
    const std::string& symbol,
    char side,
    double orderQty,
    double price,
    const std::string& ordType,
    const std::string& timeInForce
)>;

class FIXGateway : public FIX::Application, public FIX::MessageCracker {
public:
    FIXGateway();

    /// Initialize and start FIX sessions from a config file
    bool init(const std::string& configPath);

    /// Send an ExecutionReport (e.g., fill notification) back to the order sender
    bool sendExecutionReport(
        const std::string& clOrdID,
        const std::string& orderID,
        const std::string& execID,
        char execType,
        char ordStatus,
        const std::string& symbol,
        char side,
        double lastQty,
        double lastPx,
        double leavesQty
    );

    /// Register callback for incoming orders
    void onNewOrderSingle(NewOrderSingleCallback cb);

    /// Stop FIX sessions and clean up
    void stop();

    // FIX::Application interface
    void onCreate(const FIX::SessionID&) override;
    void onLogon(const FIX::SessionID&) override;
    void onLogout(const FIX::SessionID&) override;
    void toAdmin(FIX::Message&, const FIX::SessionID&) override;
    void toApp(FIX::Message&, const FIX::SessionID&) throw(FIX::DoNotSend) override;
    void fromAdmin(const FIX::Message&, const FIX::SessionID&)
        throw(FIX::FieldNotFound, FIX::IncorrectDataFormat, FIX::IncorrectTagValue, FIX::RejectLogon) override;
    void fromApp(const FIX::Message&, const FIX::SessionID&)
        throw(FIX::FieldNotFound, FIX::IncorrectDataFormat, FIX::IncorrectTagValue, FIX::UnsupportedMessageType) override;

protected:
    void onMessage(const FIX50SP2::NewOrderSingle&, const FIX::SessionID&);
    void onMessage(const FIX50SP2::OrderCancelRequest&, const FIX::SessionID&);
    void onMessage(const FIX50SP2::OrderCancelReplaceRequest&, const FIX::SessionID&);

private:
    NewOrderSingleCallback newOrderCb_;
    std::unique_ptr<FIX::SocketInitiator> initiator_;
    std::unique_ptr<FIX::SessionSettings> settings_;
    std::unique_ptr<FIX::FileStoreFactory> storeFactory_;
    std::unique_ptr<FIX::ScreenLogFactory> logFactory_;
    std::string configPath_;
    bool initialized_ = false;
};

} // namespace execution
} // namespace quantum
