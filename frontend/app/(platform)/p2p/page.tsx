"use client";

import React, { useEffect, useMemo, useState } from "react";
import { motion } from "framer-motion";
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";

type DepthPoint = {
  price: number;
  bids: number;
  asks: number;
};

type TradeRow = {
  tradeId: string;
  contributor: string;
  consumer: string;
  energy: string;
  settlement: string;
  timestamp: string;
};

export default function P2PPage() {
  const [trades, setTrades] = useState<TradeRow[]>([]);

  useEffect(() => {
    const socket = new WebSocket("ws://localhost:8080/ws");

    socket.onmessage = (event) => {
      const data = JSON.parse(event.data) as {
        type?: string;
        trade_id?: string;
        contributor_id?: string;
        energy_kw?: number;
        credit?: number;
        timestamp?: string;
      };

      if (data.type !== "EXECUTE_TRADE") return;

      const energy = data.energy_kw ?? 0;
      const credit = data.credit ?? 0;
      const settlementNum = energy > 0 ? credit / energy : 0;
      const row: TradeRow = {
        tradeId: data.trade_id ?? `TRD-${Date.now()}`,
        contributor: data.contributor_id ?? "ND_UNKNOWN",
        consumer: "GRID_POOL",
        energy: `${energy.toFixed(2)}kW`,
        settlement: `$${settlementNum.toFixed(3)}`,
        timestamp: data.timestamp ?? new Date().toISOString(),
      };
      setTrades((prev) => [row, ...prev].slice(0, 12));
    };

    return () => socket.close();
  }, []);

  const depthData: DepthPoint[] = useMemo(() => {
    const latest = trades[0];
    const mid = latest ? Number(latest.settlement.replace("$", "")) : 0.150;
    return [
      { price: Number((mid - 0.006).toFixed(3)), bids: 9.3, asks: 2.8 },
      { price: Number((mid - 0.004).toFixed(3)), bids: 8.2, asks: 3.4 },
      { price: Number((mid - 0.002).toFixed(3)), bids: 7.4, asks: 4.1 },
      { price: Number(mid.toFixed(3)), bids: 5.9, asks: 5.6 },
      { price: Number((mid + 0.002).toFixed(3)), bids: 4.1, asks: 7.2 },
      { price: Number((mid + 0.004).toFixed(3)), bids: 3.2, asks: 8.7 },
      { price: Number((mid + 0.006).toFixed(3)), bids: 2.6, asks: 9.6 },
    ];
  }, [trades]);

  const asks = useMemo(
    () => [
      { price: `$${(depthData[4]?.price ?? 0.152).toFixed(3)}`, volume: "4.2kW" },
      { price: `$${(depthData[5]?.price ?? 0.153).toFixed(3)}`, volume: "3.7kW" },
      { price: `$${(depthData[6]?.price ?? 0.154).toFixed(3)}`, volume: "5.1kW" },
      { price: `$${(depthData[6]?.price + 0.001 || 0.155).toFixed(3)}`, volume: "6.2kW" },
    ],
    [depthData],
  );

  const bids = useMemo(
    () => [
      { price: `$${(depthData[2]?.price ?? 0.149).toFixed(3)}`, volume: "8.1kW" },
      { price: `$${(depthData[1]?.price ?? 0.148).toFixed(3)}`, volume: "7.4kW" },
      { price: `$${(depthData[0]?.price ?? 0.147).toFixed(3)}`, volume: "6.9kW" },
      { price: `$${(depthData[0]?.price - 0.001 || 0.146).toFixed(3)}`, volume: "5.8kW" },
    ],
    [depthData],
  );

  const spread = useMemo(() => {
    const bestAsk = depthData[4]?.price ?? 0.152;
    const bestBid = depthData[2]?.price ?? 0.149;
    return (bestAsk - bestBid).toFixed(3);
  }, [depthData]);

  return (
    <div className="text-zinc-300">
      <header className="border-b border-zinc-900 pb-4">
        <h1 className="text-2xl font-sans font-bold tracking-tight text-white">ENERGY ESCROW &amp; P2P MARKET</h1>
      </header>

      <motion.div
        className="mt-8 space-y-6"
        initial="hidden"
        animate="show"
        variants={{ hidden: {}, show: { transition: { staggerChildren: 0.2 } } }}
      >
        <motion.div
          className="grid grid-cols-1 lg:grid-cols-3 gap-6"
          initial={{ opacity: 0, y: 30 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6 }}
        >
          <div className="lg:col-span-2 bg-[#0a0a0a] border border-zinc-900 rounded-sm p-5">
            <p className="text-xs text-zinc-500 tracking-widest mb-4">MARKET DEPTH</p>
            <div className="w-full h-[300px]">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={depthData}>
                  <defs>
                    <linearGradient id="bidGreen" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#10b981" stopOpacity={0.5} />
                      <stop offset="95%" stopColor="#10b981" stopOpacity={0.05} />
                    </linearGradient>
                    <linearGradient id="askOrange" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#f97316" stopOpacity={0.45} />
                      <stop offset="95%" stopColor="#f97316" stopOpacity={0.05} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid stroke="#333" strokeDasharray="2 4" />
                  <XAxis dataKey="price" tick={{ fill: "#a1a1aa", fontSize: 11 }} />
                  <YAxis tick={{ fill: "#a1a1aa", fontSize: 11 }} />
                  <Tooltip contentStyle={{ backgroundColor: "#050505", border: "1px solid #18181b" }} />
                  <Area dataKey="bids" stroke="#10b981" fill="url(#bidGreen)" isAnimationActive={false} />
                  <Area dataKey="asks" stroke="#f97316" fill="url(#askOrange)" isAnimationActive={false} />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          </div>

          <div className="bg-[#0a0a0a] border border-zinc-900 rounded-sm p-5">
            <p className="text-xs text-zinc-500 tracking-widest mb-3">ORDER BOOK</p>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <p className="text-[10px] text-orange-500 mb-2">ASKS (SELLS)</p>
                <div className="space-y-1">
                  {asks.map((ask) => (
                    <div key={`${ask.price}-${ask.volume}`} className="text-xs text-orange-400">
                      {ask.price} | {ask.volume}
                    </div>
                  ))}
                </div>
              </div>
              <div>
                <p className="text-[10px] text-emerald-500 mb-2">BIDS (BUYS)</p>
                <div className="space-y-1">
                  {bids.map((bid) => (
                    <div key={`${bid.price}-${bid.volume}`} className="text-xs text-emerald-400">
                      {bid.price} | {bid.volume}
                    </div>
                  ))}
                </div>
              </div>
            </div>
            <div className="mt-4 pt-3 border-t border-zinc-900 text-xs text-zinc-300">SPREAD: ${spread}</div>
          </div>
        </motion.div>

        <motion.div
          className="bg-[#0a0a0a] border border-zinc-900 rounded-sm p-5"
          initial={{ opacity: 0, y: 30 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6 }}
        >
          <p className="text-xs text-zinc-500 tracking-widest mb-4">REAL-TIME SETTLEMENT LEDGER</p>
          <div className="grid grid-cols-6 text-[10px] text-zinc-500 border-b border-zinc-900 pb-2 tracking-widest">
            <span>TRADE_ID</span>
            <span>CONTRIBUTOR_NODE</span>
            <span>CONSUMER_NODE</span>
            <span>ENERGY_TRANSFER</span>
            <span>SETTLEMENT_PRICE</span>
            <span>TIMESTAMP</span>
          </div>
          <div className="mt-2 space-y-1">
            {trades.map((trade, index) => (
              <motion.div
                key={trade.tradeId}
                className="grid grid-cols-6 text-xs border-b border-zinc-900 py-2"
                initial={{ opacity: 0, backgroundColor: "rgba(255,255,255,0.9)" }}
                animate={{ opacity: 1, backgroundColor: "rgba(16,185,129,0.06)" }}
                transition={{ duration: 0.5, delay: index * 0.08 }}
              >
                <span className="text-zinc-300">{trade.tradeId}</span>
                <span className="text-emerald-500">{trade.contributor}</span>
                <span className="text-zinc-300">{trade.consumer}</span>
                <span className="text-emerald-400">{trade.energy}</span>
                <span className="text-zinc-100">{trade.settlement}</span>
                <span className="text-zinc-500">{trade.timestamp}</span>
              </motion.div>
            ))}
            {trades.length === 0 && (
              <div className="text-xs text-zinc-500 py-4">Awaiting EXECUTE_TRADE events from websocket stream...</div>
            )}
          </div>
        </motion.div>
      </motion.div>
    </div>
  );
}
