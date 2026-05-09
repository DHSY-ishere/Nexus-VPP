"use client";

import React, { useEffect, useState } from "react";
import { motion } from "framer-motion";
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import Globe from "./components/Globe";

type TelemetryPoint = {
  time: string;
  consumption: number;
  replenishment: number;
  frequency: number;
};

export default function DashboardPage() {
  const [gridFrequency, setGridFrequency] = useState(50.0);
  const [telemetry, setTelemetry] = useState<TelemetryPoint[]>([]);
  const [algoState, setAlgoState] = useState(
    "> Waiting for telemetry stream...\n> Establishing websocket tunnel...\n> Threshold pending...",
  );

  useEffect(() => {
    const socket = new WebSocket("ws://localhost:8080/ws");

    socket.onopen = () => {
      setAlgoState("> Stream connected.\n> Analyzing strain...\n> Threshold locked at 0.84.");
    };

    socket.onmessage = (event) => {
      const data = JSON.parse(event.data) as {
        local_frequency_hz?: number;
        current_consumption_kw?: number;
        replenishment_rate_kw?: number;
      };

      if (typeof data.local_frequency_hz !== "number") return;

      const now = new Date();
      const point: TelemetryPoint = {
        time: now.toLocaleTimeString("en-US", { minute: "2-digit", second: "2-digit" }),
        consumption: Number((data.current_consumption_kw ?? 0).toFixed(2)),
        replenishment: Number((data.replenishment_rate_kw ?? 0).toFixed(2)),
        frequency: Number(data.local_frequency_hz.toFixed(3)),
      };

      setGridFrequency(point.frequency);
      setTelemetry((prev) => [...prev.slice(-29), point]);
      setAlgoState(
        point.frequency < 49.9
          ? "> Analyzing strain...\n> Moonlight provisioning active...\n> Threshold locked at 0.84."
          : "> Analyzing strain...\n> Grid nominal and stable...\n> Threshold locked at 0.84.",
      );
    };

    socket.onerror = () => {
      setAlgoState("> Stream error detected...\n> Retrying websocket tunnel...\n> Threshold locked at 0.84.");
    };

    socket.onclose = () => {
      setAlgoState("> Stream disconnected.\n> Awaiting backend restore...\n> Threshold locked at 0.84.");
    };

    return () => socket.close();
  }, []);

  const isStrained = gridFrequency < 49.9;

  return (
    <div className="text-zinc-300">
      <header className="border-b border-zinc-900 pb-4 flex items-end justify-between">
        <div>
          <h1 className="text-xl font-sans font-bold text-white tracking-tight">GRID OBSERVABILITY</h1>
        </div>
        <div className="text-right">
          <p className="text-[10px] text-zinc-500 tracking-widest">LIVE GRID FREQUENCY</p>
          <p className={`text-4xl font-sans font-black ${isStrained ? "text-orange-500 animate-pulse" : "text-emerald-400 neon-glow"}`}>
            {gridFrequency.toFixed(3)}
          </p>
        </div>
      </header>

      <motion.div
        className="mt-8 space-y-6"
        initial="hidden"
        animate="show"
        variants={{ hidden: {}, show: { transition: { staggerChildren: 0.2 } } }}
      >
        <motion.div
          className="bg-[#0a0a0a] border border-zinc-900 rounded-sm p-5"
          initial={{ opacity: 0, y: 30 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6 }}
        >
          <div className="flex items-center justify-between mb-4">
            <p className="text-xs text-zinc-500 tracking-widest">TELEMETRY AGGREGATION</p>
            <span className={`text-[10px] px-2 py-1 rounded-sm ${isStrained ? "text-orange-500 bg-orange-500/10" : "text-emerald-500 bg-emerald-500/10"}`}>
              {isStrained ? "DISCHARGE" : "NOMINAL"}
            </span>
          </div>
          <div className="w-full h-[400px]">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={telemetry}>
                <defs>
                  <linearGradient id="dashGreen" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#10b981" stopOpacity={0.4} />
                    <stop offset="95%" stopColor="#10b981" stopOpacity={0.02} />
                  </linearGradient>
                </defs>
                <CartesianGrid stroke="#333" strokeDasharray="2 4" />
                <XAxis dataKey="time" hide />
                <YAxis hide />
                <Tooltip contentStyle={{ backgroundColor: "#050505", border: "1px solid #18181b", color: "#d4d4d8" }} />
                <Area type="stepAfter" dataKey="consumption" stroke="#3f3f46" fill="#3f3f46" fillOpacity={0.15} isAnimationActive={false} />
                <Area type="stepAfter" dataKey="replenishment" stroke="#10b981" fill="url(#dashGreen)" isAnimationActive={false} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </motion.div>

        <motion.div
          className="grid grid-cols-1 lg:grid-cols-3 gap-6"
          initial={{ opacity: 0, y: 30 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6 }}
        >
          <div className="bg-[#0a0a0a] border border-zinc-900 rounded-sm p-5 lg:col-span-2">
            <p className="text-xs text-zinc-500 tracking-widest mb-2">NODE MESH STATUS</p>
            <p className="text-2xl font-sans font-bold text-white mb-4">ACTIVE NODES: 10,492</p>
            <div className="w-full h-[500px] bg-[#050505] rounded-sm border border-zinc-900 relative overflow-hidden">
              <div className="absolute top-4 left-4 z-10 bg-black/50 backdrop-blur border border-zinc-800 text-sm px-4 py-2 text-white rounded-sm">
                Search Region or Node ID...
              </div>
              <Globe />
            </div>
          </div>

          <div className="bg-[#0a0a0a] border border-zinc-900 rounded-sm p-5 flex flex-col justify-center">
            <p className="text-xs text-zinc-500 tracking-widest mb-2">REPLENISHMENT EFFICIENCY</p>
            <p className="text-6xl font-sans font-black text-white">84%</p>
            <p className="text-emerald-500 mt-2">SOLAR_INDEX_NOMINAL</p>
          </div>

          <div className="bg-[#0a0a0a] border border-zinc-900 rounded-sm p-5">
            <p className="text-xs text-zinc-500 tracking-widest mb-3">ML ALGORITHM STATE</p>
            <pre className="text-emerald-500 text-xs leading-6 whitespace-pre-wrap">
              {algoState}
            </pre>
          </div>
        </motion.div>
      </motion.div>
    </div>
  );
}
