@0x8a7b3c2d1e0f4a5b;

struct Order {
    id @0 :UInt64;
    symbol @1 :Text;
    price @2 :UInt64;
    quantity @3 :UInt64;
    side @4 :UInt8;
    timestamp @5 :UInt64;
    accountId @6 :UInt32;
}

struct Trade {
    tradeId @0 :UInt64;
    buyOrderId @1 :UInt64;
    sellOrderId @2 :UInt64;
    instrumentId @3 :UInt32;
    price @4 :UInt64;
    quantity @5 :UInt64;
    timestamp @6 :UInt64;
}

struct RiskResult {
    approved @0 :Bool;
    rejectReason @1 :Text;
    latencyNs @2 :UInt64;
}
