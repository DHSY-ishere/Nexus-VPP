"use client";

import React, { useEffect, useState } from "react";
import { LineChart, Line, ResponsiveContainer, YAxis, Tooltip } from "recharts";

type TelemetryPoint = {
  local_frequency_hz: number;
};

type TradeEvent = {
  type: "EXECUTE_TRADE";
  contributor_id: string;
  energy_kw: number;
  credit: number;
};

export default function NexusDashboard() {
  const [metrics, setMetrics] = useState<TelemetryPoint[]>([]);
  const [gridFreq, setGridFreq] = useState(50.0);
  const [wallet, setWallet] = useState(0.0);
  const [trades, setTrades] = useState<TradeEvent[]>([]);

  useEffect(() => {
    const socket = new WebSocket("ws://localhost:8080/ws");
    socket.onmessage = (event) => {
      const data = JSON.parse(event.data) as TelemetryPoint | TradeEvent;

      if ("type" in data && data.type === "EXECUTE_TRADE") {
        setWallet((prev) => prev + data.credit);
        setTrades((prev) => [data, ...prev].slice(0, 5));
      } else if ("local_frequency_hz" in data) {
        setGridFreq(data.local_frequency_hz);
        setMetrics((prev) => [...prev.slice(-30), data]);
      }
    };
    return () => socket.close();
  }, []);

  const isStrained = gridFreq < 49.8;

  return (
    <div className="min-h-screen bg-[#0a0a0a] text-gray-300 p-8 font-mono select-none">
      <header className="border-b border-gray-800 pb-4 mb-8 flex justify-between items-end">
        <div>
          <h1 className="text-2xl font-bold tracking-widest text-white">NEXUS_VPP</h1>
          <p className="text-xs text-gray-500 uppercase tracking-widest mt-1">Orchestrator Terminal</p>
        </div>
        <div className="text-right">
          <p className="text-[10px] text-gray-500 uppercase mb-1">Grid Frequency</p>
          <p className={`text-3xl font-black ${isStrained ? "text-orange-500 animate-pulse" : "text-emerald-400"}`}>
            {gridFreq.toFixed(3)} <span className="text-sm font-normal">Hz</span>
          </p>
        </div>
      </header>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="lg:col-span-2 bg-[#111111] border border-gray-800 p-6 rounded-sm">
          <div className="flex justify-between items-center mb-6">
            <h2 className="text-xs font-bold text-gray-500 uppercase tracking-widest">Live Aggregation</h2>
            <div
              className={`px-2 py-1 text-[10px] uppercase font-bold rounded ${
                isStrained ? "bg-orange-500/20 text-orange-500" : "bg-emerald-500/20 text-emerald-500"
              }`}
            >
              {isStrained ? "EMERGENCY DISCHARGE" : "NOMINAL"}
            </div>
          </div>
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={metrics}>
                <Line
                  type="stepAfter"
                  dataKey="local_frequency_hz"
                  stroke={isStrained ? "#f97316" : "#10b981"}
                  strokeWidth={2}
                  dot={false}
                  isAnimationActive={false}
                />
                <YAxis domain={["dataMin - 0.2", "dataMax + 0.2"]} hide />
                <Tooltip contentStyle={{ backgroundColor: "#000", border: "1px solid #333" }} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </div>

        <div className="flex flex-col gap-6">
          <div className="bg-[#111111] border border-gray-800 p-6 rounded-sm">
            <h2 className="text-xs font-bold text-gray-500 uppercase tracking-widest mb-4">P2P Escrow Balance</h2>
            <div className="text-4xl font-light text-white">${wallet.toFixed(2)}</div>
            <p className="text-[10px] text-gray-500 mt-2">TOTAL CREDITS EARNED</p>
          </div>

          <div className="bg-[#111111] border border-gray-800 p-6 rounded-sm flex-1">
            <h2 className="text-xs font-bold text-gray-500 uppercase tracking-widest mb-4">Settlement Ledger</h2>
            <div className="space-y-3">
              {trades.length === 0 ? (
                <div className="text-xs text-gray-600 text-center py-4">AWAITING GRID STRAIN...</div>
              ) : (
                trades.map((t, i) => (
                  <div key={i} className="flex justify-between items-center border-b border-gray-800 pb-2">
                    <div>
                      <div className="text-[10px] text-gray-500">{t.contributor_id}</div>
                      <div className="text-xs text-gray-300">+{t.energy_kw.toFixed(2)}kW</div>
                    </div>
                    <div className="text-emerald-400 text-sm font-bold">+${t.credit.toFixed(2)}</div>
                  </div>
                ))
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
