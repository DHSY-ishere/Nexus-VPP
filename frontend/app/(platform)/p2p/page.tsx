"use client";

import React, { useEffect, useMemo, useState } from "react";
import { motion } from "framer-motion";
import PageHeader from "../components/PageHeader";
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

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
  blockHash?: string;
};

export default function P2PPage() {
  const [trades, setTrades] = useState<TradeRow[]>([]);
  const [gridFrequency, setGridFrequency] = useState(50.0);
  const [sessionStats, setSessionStats] = useState({ totalEnergy: 0, totalProfit: 0 });

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
        local_frequency_hz?: number;
        block_hash?: string;
      };

      // Update frequency
      if (typeof data.local_frequency_hz === "number") {
        setGridFrequency(data.local_frequency_hz);
      }

      if (data.type !== "EXECUTE_TRADE" && data.type !== "TRADE_EVENT") return;

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
        blockHash: data.block_hash,
      };
      
      setSessionStats(prev => ({
        totalEnergy: prev.totalEnergy + energy,
        totalProfit: prev.totalProfit + credit
      }));

      setTrades((prev) => [row, ...prev].slice(0, 12));
    };

    return () => socket.close();
  }, []);

  const [chainStatus, setChainStatus] = useState<{valid: boolean, blocks: number} | null>(null);

  useEffect(() => {
    const fetchChainStatus = async () => {
      try {
        const res = await fetch("http://localhost:8080/chain/verify");
        if (res.ok) {
          const data = await res.json();
          setChainStatus(data);
        }
      } catch (e) {
        console.error(e);
      }
    };
    fetchChainStatus();
    const interval = setInterval(fetchChainStatus, 10000);
    return () => clearInterval(interval);
  }, []);

  const isStrained = gridFrequency < 49.9;

  const depthData: DepthPoint[] = useMemo(() => {
    const latest = trades[0];
    const basePrice = latest
      ? Number(latest.settlement.replace("$", ""))
      : 0.15;
    // Price volatility: $0.15-$0.85 when strained
    const volatilityFactor = isStrained ? 5.67 : 1.0; // 0.85 / 0.15 ≈ 5.67
    const volatilePrice = basePrice * volatilityFactor;
    const midPrice = Math.min(volatilePrice, 0.85);

    return [
      { price: Number((midPrice - 0.006).toFixed(3)), bids: 9.3, asks: 2.8 },
      { price: Number((midPrice - 0.004).toFixed(3)), bids: 8.2, asks: 3.4 },
      { price: Number((midPrice - 0.002).toFixed(3)), bids: 7.4, asks: 4.1 },
      { price: Number(midPrice.toFixed(3)), bids: 5.9, asks: 5.6 },
      { price: Number((midPrice + 0.002).toFixed(3)), bids: 4.1, asks: 7.2 },
      { price: Number((midPrice + 0.004).toFixed(3)), bids: 3.2, asks: 8.7 },
      { price: Number((midPrice + 0.006).toFixed(3)), bids: 2.6, asks: 9.6 },
    ];
  }, [trades, isStrained]);

  const asks = useMemo(
    () => [
      {
        price: `$${(depthData[4]?.price ?? 0.152).toFixed(3)}`,
        volume: "4.2kW",
      },
      {
        price: `$${(depthData[5]?.price ?? 0.153).toFixed(3)}`,
        volume: "3.7kW",
      },
      {
        price: `$${(depthData[6]?.price ?? 0.154).toFixed(3)}`,
        volume: "5.1kW",
      },
      {
        price: `$${(depthData[6]?.price + 0.001 || 0.155).toFixed(3)}`,
        volume: "6.2kW",
      },
    ],
    [depthData],
  );

  const bids = useMemo(
    () => [
      {
        price: `$${(depthData[2]?.price ?? 0.149).toFixed(3)}`,
        volume: "8.1kW",
      },
      {
        price: `$${(depthData[1]?.price ?? 0.148).toFixed(3)}`,
        volume: "7.4kW",
      },
      {
        price: `$${(depthData[0]?.price ?? 0.147).toFixed(3)}`,
        volume: "6.9kW",
      },
      {
        price: `$${(depthData[0]?.price - 0.001 || 0.146).toFixed(3)}`,
        volume: "5.8kW",
      },
    ],
    [depthData],
  );

  const spread = useMemo(() => {
    const bestAsk = depthData[4]?.price ?? 0.152;
    const bestBid = depthData[2]?.price ?? 0.149;
    return (bestAsk - bestBid).toFixed(3);
  }, [depthData]);

  const handleManualTrade = (type: "BUY" | "SELL") => {
    const energy = 3.5 + Math.random() * 5.0;
    const isStrainedNow = gridFrequency < 49.9;
    const price = isStrainedNow ? 0.85 : 0.15;
    const credit = energy * price;
    
    const row: TradeRow = {
      tradeId: `MANUAL-${type}-${Date.now()}`,
      contributor: type === "SELL" ? "USER_NODE" : "GRID_POOL",
      consumer: type === "BUY" ? "USER_NODE" : "GRID_POOL",
      energy: `${energy.toFixed(2)}kW`,
      settlement: `$${price.toFixed(3)}`,
      timestamp: new Date().toISOString(),
    };
    
    setSessionStats(prev => ({
      totalEnergy: prev.totalEnergy + energy,
      totalProfit: prev.totalProfit + credit
    }));
    
    setTrades(prev => [row, ...prev].slice(0, 12));
  };

  const strain = Math.max(0, (50 - gridFrequency) * 10).toFixed(2);
  const dynamicPrice = (0.15 + (Number(strain) * 0.05)).toFixed(2);

  const marketPrice = Math.min(isStrained ? 0.85 : 0.15, depthData[3]?.price ?? 0.15);

  return (
    <div>
      <PageHeader
        eyebrow="Energy escrow"
        title="P2P"
        accent="market"
        description="Live order book and a settlement ledger where every trade is hash-chained."
      >
        <div className="glass flex items-center gap-4 px-6 py-5">
          {chainStatus ? (
            <>
              <span
                className={`h-2.5 w-2.5 animate-pulse rounded-full ${chainStatus.valid ? "bg-emerald-400" : "bg-rose-400"}`}
              />
              <div>
                <p className={`text-sm font-medium ${chainStatus.valid ? "text-emerald-300" : "text-rose-300"}`}>
                  {chainStatus.valid ? "Chain verified" : "Chain integrity failure"}
                </p>
                <p className="mt-1 text-xs text-white/50">{chainStatus.blocks} blocks</p>
              </div>
            </>
          ) : (
            <p className="text-sm text-white/50">Connecting to chain...</p>
          )}
        </div>
        <div className="glass px-7 py-5">
          <p className="label">Market price</p>
          <p
            className={`stat-num mt-3 text-4xl ${isStrained ? "text-amber-300 animate-pulse" : "text-emerald-300"}`}
          >
            ${marketPrice.toFixed(2)}
            <span className="ml-1 text-base font-normal text-white/40">/kW</span>
          </p>
        </div>
      </PageHeader>

      <motion.div
        className="mt-12 grid grid-cols-1 gap-8 lg:grid-cols-3"
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
      >
        <section className="glass p-7 md:p-8">
          <p className="label">Session accumulator</p>
          <dl className="mt-8 space-y-6">
            <div className="flex items-baseline justify-between gap-4">
              <dt className="text-sm text-white/60">Energy traded</dt>
              <dd className="stat-num text-2xl text-sky-300">
                {sessionStats.totalEnergy.toFixed(2)} <span className="text-sm font-normal text-white/40">kWh</span>
              </dd>
            </div>
            <div className="flex items-baseline justify-between gap-4">
              <dt className="text-sm text-white/60">P2P profit</dt>
              <dd className="stat-num text-2xl text-emerald-300">${sessionStats.totalProfit.toFixed(2)}</dd>
            </div>
          </dl>
        </section>

        <section className="glass flex flex-col p-7 md:p-8">
          <p className="label">Manual trade</p>
          <p className="mt-3 text-sm text-white/55">Place a test order at the current grid price.</p>
          <div className="mt-auto flex gap-4 pt-8">
            <button type="button" onClick={() => handleManualTrade("BUY")} className="btn btn-volt flex-1">
              Buy energy
            </button>
            <button type="button" onClick={() => handleManualTrade("SELL")} className="btn btn-amber flex-1">
              Sell energy
            </button>
          </div>
        </section>

        <section className="glass glass-tint p-7 md:p-8">
          <p className="label text-emerald-200/80">Live balancing algorithm</p>
          <dl className="mt-8 space-y-4 text-sm">
            <div className="flex justify-between gap-4">
              <dt className="text-white/60">Grid deficit</dt>
              <dd className="font-mono text-white">{strain} Hz</dd>
            </div>
            <div className="flex justify-between gap-4">
              <dt className="text-white/60">Dynamic price</dt>
              <dd className="font-mono text-white">${dynamicPrice} / kWh</dd>
            </div>
          </dl>
          <p className="mt-6 border-t border-white/10 pt-5 font-mono text-xs leading-6 text-white/50">
            price = P_base + (Δf × K_p)
            <br />
            profit = energy × price
          </p>
        </section>
      </motion.div>

      <motion.div
        className="mt-8 space-y-8"
        initial="hidden"
        animate="show"
        variants={{ hidden: {}, show: { transition: { staggerChildren: 0.12 } } }}
      >
        <motion.div
          className="grid grid-cols-1 gap-8 lg:grid-cols-3"
          initial={{ opacity: 0, y: 24 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6 }}
        >
          <section className="glass p-7 md:p-8 lg:col-span-2">
            <div className="mb-8 flex flex-wrap items-center justify-between gap-4">
              <p className="label">Market depth</p>
              <div className="flex gap-5 text-sm text-white/65">
                <span className="flex items-center gap-2">
                  <span className="h-2.5 w-2.5 rounded-full bg-emerald-400" />
                  Bids
                </span>
                <span className="flex items-center gap-2">
                  <span className="h-2.5 w-2.5 rounded-full bg-amber-400" />
                  Asks
                </span>
              </div>
            </div>
            <div className="glass-inset p-4" style={{ height: 340 }}>
              <ResponsiveContainer width="100%" height={308}>
                <AreaChart data={depthData} margin={{ top: 8, right: 12, left: -8, bottom: 0 }}>
                  <defs>
                    <linearGradient id="bidGreen" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#34d399" stopOpacity={0.5} />
                      <stop offset="95%" stopColor="#34d399" stopOpacity={0.03} />
                    </linearGradient>
                    <linearGradient id="askAmber" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#fbbf24" stopOpacity={0.42} />
                      <stop offset="95%" stopColor="#fbbf24" stopOpacity={0.03} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid stroke="rgba(255,255,255,0.06)" strokeDasharray="2 6" />
                  <XAxis dataKey="price" tick={{ fontSize: 11 }} tickLine={false} axisLine={false} />
                  <YAxis tick={{ fontSize: 11 }} tickLine={false} axisLine={false} />
                  <Tooltip />
                  <Area dataKey="bids" stroke="#34d399" strokeWidth={2} fill="url(#bidGreen)" isAnimationActive={false} />
                  <Area dataKey="asks" stroke="#fbbf24" strokeWidth={2} fill="url(#askAmber)" isAnimationActive={false} />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          </section>

          <section className="glass p-7 md:p-8">
            <p className="label mb-8">Order book</p>
            <div className="grid grid-cols-2 gap-6">
              <div>
                <p className="mb-4 text-xs font-medium uppercase tracking-[0.12em] text-amber-300">Asks</p>
                <ul className="space-y-3 font-mono text-sm text-amber-200/90">
                  {asks.map((ask) => (
                    <li key={`${ask.price}-${ask.volume}`} className="flex justify-between gap-2">
                      <span>{ask.price}</span>
                      <span className="text-white/45">{ask.volume}</span>
                    </li>
                  ))}
                </ul>
              </div>
              <div>
                <p className="mb-4 text-xs font-medium uppercase tracking-[0.12em] text-emerald-300">Bids</p>
                <ul className="space-y-3 font-mono text-sm text-emerald-200/90">
                  {bids.map((bid) => (
                    <li key={`${bid.price}-${bid.volume}`} className="flex justify-between gap-2">
                      <span>{bid.price}</span>
                      <span className="text-white/45">{bid.volume}</span>
                    </li>
                  ))}
                </ul>
              </div>
            </div>
            <div className="mt-8 flex items-center justify-between border-t border-white/10 pt-5 text-sm">
              <span className="text-white/55">Spread</span>
              <span className="font-mono text-white">${spread}</span>
            </div>
          </section>
        </motion.div>

        <motion.section
          className="glass p-7 md:p-8"
          initial={{ opacity: 0, y: 24 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6 }}
        >
          <p className="label mb-8">Real-time settlement ledger</p>
          <div className="overflow-x-auto">
            <table className="table-glass min-w-[900px]">
              <thead>
                <tr>
                  <th>Trade</th>
                  <th>Block hash</th>
                  <th>Contributor</th>
                  <th>Consumer</th>
                  <th>Energy</th>
                  <th>Price</th>
                  <th>Time</th>
                </tr>
              </thead>
              <tbody>
                {trades.map((trade) => (
                  <tr key={trade.tradeId}>
                    <td className="font-mono text-white/80">{trade.tradeId}</td>
                    <td className="font-mono text-emerald-300/90">
                      {trade.blockHash ? `${trade.blockHash.slice(0, 12)}…` : "pending"}
                    </td>
                    <td className="text-emerald-300">{trade.contributor}</td>
                    <td>{trade.consumer}</td>
                    <td className="font-mono text-sky-300">{trade.energy}</td>
                    <td className="font-mono text-white">{trade.settlement}</td>
                    <td className="font-mono text-white/45">{trade.timestamp.slice(11, 19)}</td>
                  </tr>
                ))}
                {trades.length === 0 && (
                  <tr>
                    <td colSpan={7} className="py-10 text-center text-white/45">
                      Waiting for trades from the stream.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </motion.section>
      </motion.div>
    </div>
  );
}
