import React, { useState } from 'react';

export interface BlotterOrder {
  id: string;
  symbol: string;
  side: 'BUY' | 'SELL';
  type: 'MARKET' | 'LIMIT' | 'STOP' | 'TWAP' | 'VWAP';
  qty: number;
  price: number;
  filledQty: number;
  avgPrice: number;
  status: 'NEW' | 'WORKING' | 'PARTIAL' | 'FILLED' | 'CANCELLED' | 'REJECTED';
  timestamp: string;
  account: string;
  strategy: string;
}

interface OrderBlotterProps {
  orders?: BlotterOrder[];
  onCancelOrder?: (orderId: string) => void;
}

export const OrderBlotter: React.FC<OrderBlotterProps> = ({ orders = [], onCancelOrder }) => {
  const [filter, setFilter] = useState<'ALL' | 'WORKING' | 'FILLED'>('ALL');

  const defaultOrders: BlotterOrder[] = [
    {
      id: 'ORD-9021',
      symbol: 'BTC/USD',
      side: 'BUY',
      type: 'LIMIT',
      qty: 2.5,
      price: 64450.0,
      filledQty: 1.0,
      avgPrice: 64448.5,
      status: 'PARTIAL',
      timestamp: new Date().toLocaleTimeString(),
      account: 'PROP-DESK-1',
      strategy: 'ALPHA-STATARB',
    },
    {
      id: 'ORD-9022',
      symbol: 'ETH/USD',
      side: 'SELL',
      type: 'TWAP',
      qty: 15.0,
      price: 3455.0,
      filledQty: 15.0,
      avgPrice: 3454.2,
      status: 'FILLED',
      timestamp: new Date(Date.now() - 120000).toLocaleTimeString(),
      account: 'QUANT-FUND-A',
      strategy: 'TWAP-SLICER',
    },
  ];

  const activeOrders = orders.length > 0 ? orders : defaultOrders;

  const filteredOrders = activeOrders.filter((ord) => {
    if (filter === 'WORKING') return ord.status === 'WORKING' || ord.status === 'PARTIAL';
    if (filter === 'FILLED') return ord.status === 'FILLED';
    return true;
  });

  const getStatusBadge = (status: BlotterOrder['status']) => {
    const styles: Record<string, string> = {
      NEW: 'bg-blue-900/50 text-blue-300 border-blue-700/50',
      WORKING: 'bg-amber-900/50 text-amber-300 border-amber-700/50',
      PARTIAL: 'bg-purple-900/50 text-purple-300 border-purple-700/50',
      FILLED: 'bg-emerald-900/50 text-emerald-300 border-emerald-700/50',
      CANCELLED: 'bg-slate-800 text-slate-400 border-slate-700',
      REJECTED: 'bg-rose-900/50 text-rose-300 border-rose-700/50',
    };
    return (
      <span className={`px-2 py-0.5 rounded text-[10px] font-semibold border ${styles[status] || styles.NEW}`}>
        {status}
      </span>
    );
  };

  return (
    <div className="bg-slate-900 border border-slate-800 rounded-lg p-4 font-mono text-xs">
      <div className="flex justify-between items-center mb-3">
        <div className="flex items-center space-x-3">
          <h3 className="font-bold text-slate-200 text-sm tracking-wide">Institutional Order Blotter</h3>
          <span className="bg-slate-800 text-slate-400 px-2 py-0.5 rounded text-[11px]">
            {filteredOrders.length} Orders
          </span>
        </div>

        <div className="flex space-x-1 bg-slate-950 p-1 rounded-md border border-slate-800">
          {(['ALL', 'WORKING', 'FILLED'] as const).map((tab) => (
            <button
              key={tab}
              onClick={() => setFilter(tab)}
              className={`px-3 py-1 rounded text-[11px] transition-colors ${
                filter === tab ? 'bg-slate-800 text-emerald-400 font-semibold' : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              {tab}
            </button>
          ))}
        </div>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full text-left border-collapse">
          <thead>
            <tr className="border-b border-slate-800 text-slate-400 text-[11px]">
              <th className="py-2 px-3">Order ID</th>
              <th className="py-2 px-3">Time</th>
              <th className="py-2 px-3">Symbol</th>
              <th className="py-2 px-3">Side</th>
              <th className="py-2 px-3">Type</th>
              <th className="py-2 px-3 text-right">Qty</th>
              <th className="py-2 px-3 text-right">Price</th>
              <th className="py-2 px-3 text-right">Filled</th>
              <th className="py-2 px-3 text-right">Avg Fill</th>
              <th className="py-2 px-3 text-center">Status</th>
              <th className="py-2 px-3 text-right">Action</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800/50 text-slate-300">
            {filteredOrders.map((ord) => (
              <tr key={ord.id} className="hover:bg-slate-800/40 transition-colors">
                <td className="py-2 px-3 text-slate-400">{ord.id}</td>
                <td className="py-2 px-3 text-slate-500">{ord.timestamp}</td>
                <td className="py-2 px-3 font-semibold text-slate-200">{ord.symbol}</td>
                <td className="py-2 px-3">
                  <span className={ord.side === 'BUY' ? 'text-emerald-400 font-semibold' : 'text-rose-400 font-semibold'}>
                    {ord.side}
                  </span>
                </td>
                <td className="py-2 px-3 text-slate-400">{ord.type}</td>
                <td className="py-2 px-3 text-right">{ord.qty.toFixed(2)}</td>
                <td className="py-2 px-3 text-right">${ord.price.toFixed(2)}</td>
                <td className="py-2 px-3 text-right">{ord.filledQty.toFixed(2)}</td>
                <td className="py-2 px-3 text-right">
                  {ord.avgPrice > 0 ? `$${ord.avgPrice.toFixed(2)}` : '-'}
                </td>
                <td className="py-2 px-3 text-center">{getStatusBadge(ord.status)}</td>
                <td className="py-2 px-3 text-right">
                  {(ord.status === 'WORKING' || ord.status === 'PARTIAL' || ord.status === 'NEW') && (
                    <button
                      onClick={() => onCancelOrder && onCancelOrder(ord.id)}
                      className="bg-rose-950/60 hover:bg-rose-900/80 text-rose-300 px-2 py-0.5 rounded border border-rose-800/50 text-[10px]"
                    >
                      Cancel
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
};

export default OrderBlotter;
