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
import PageHeader from "../components/PageHeader";

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
      setAlgoState(
        "> Stream connected.\n> Analyzing strain...\n> Threshold locked at 0.84.",
      );
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
        time: now.toLocaleTimeString("en-US", {
          minute: "2-digit",
          second: "2-digit",
        }),
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
      setAlgoState(
        "> Stream error detected...\n> Retrying websocket tunnel...\n> Threshold locked at 0.84.",
      );
    };

    socket.onclose = () => {
      setAlgoState(
        "> Stream disconnected.\n> Awaiting backend restore...\n> Threshold locked at 0.84.",
      );
    };

    return () => socket.close();
  }, []);

  const isStrained = gridFrequency < 49.9;

  return (
    <div>
      <PageHeader
        eyebrow="Live telemetry"
        title="Grid"
        accent="observability"
        description="Every node's consumption and solar replenishment, streamed live from the orchestrator."
      >
        <div className="glass flex items-center gap-6 px-7 py-5">
          <div>
            <p className="label">Grid frequency</p>
            <p
              className={`stat-num mt-3 text-4xl ${isStrained ? "text-amber-300 animate-pulse" : "text-emerald-300"}`}
            >
              {gridFrequency.toFixed(3)}
              <span className="ml-2 text-base font-normal text-white/40">Hz</span>
            </p>
          </div>
          <span className={`pill pill-tint ${isStrained ? "[--tint:var(--amber)]" : ""}`}>
            <span className="h-1.5 w-1.5 rounded-full bg-current" />
            {isStrained ? "Discharging" : "Nominal"}
          </span>
        </div>
      </PageHeader>

      <motion.div
        className="mt-12 space-y-8"
        initial="hidden"
        animate="show"
        variants={{
          hidden: {},
          show: { transition: { staggerChildren: 0.12 } },
        }}
      >
        <motion.section
          className="glass p-7 md:p-8"
          initial={{ opacity: 0, y: 24 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6 }}
        >
          <div className="mb-8 flex flex-wrap items-center justify-between gap-4">
            <div>
              <p className="label">Telemetry aggregation</p>
              <p className="mt-2 text-sm text-white/55">Consumption against solar replenishment, last 30 samples</p>
            </div>
            <div className="flex gap-5 text-sm text-white/65">
              <span className="flex items-center gap-2">
                <span className="h-2.5 w-2.5 rounded-full bg-sky-400" />
                Consumption
              </span>
              <span className="flex items-center gap-2">
                <span className="h-2.5 w-2.5 rounded-full bg-emerald-400" />
                Replenishment
              </span>
            </div>
          </div>
          <div className="glass-inset p-4" style={{ height: 380 }}>
            <ResponsiveContainer width="100%" height={348}>
              <AreaChart data={telemetry} margin={{ top: 8, right: 8, left: 8, bottom: 0 }}>
                <defs>
                  <linearGradient id="dashGreen" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#34d399" stopOpacity={0.45} />
                    <stop offset="95%" stopColor="#34d399" stopOpacity={0.02} />
                  </linearGradient>
                  <linearGradient id="dashSky" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#60a5fa" stopOpacity={0.28} />
                    <stop offset="95%" stopColor="#60a5fa" stopOpacity={0.02} />
                  </linearGradient>
                </defs>
                <CartesianGrid stroke="rgba(255,255,255,0.06)" strokeDasharray="2 6" />
                <XAxis dataKey="time" hide />
                <YAxis hide />
                <Tooltip />
                <Area
                  type="monotone"
                  dataKey="consumption"
                  stroke="#60a5fa"
                  strokeWidth={2}
                  fill="url(#dashSky)"
                  isAnimationActive={false}
                />
                <Area
                  type="monotone"
                  dataKey="replenishment"
                  stroke="#34d399"
                  strokeWidth={2}
                  fill="url(#dashGreen)"
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </motion.section>

        <motion.div
          className="grid grid-cols-1 gap-8 lg:grid-cols-3"
          initial={{ opacity: 0, y: 24 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6 }}
        >
          <section className="glass p-7 md:p-8 lg:col-span-2">
            <div className="mb-8 flex flex-wrap items-end justify-between gap-4">
              <div>
                <p className="label">Node mesh status</p>
                <p className="stat-num mt-4 text-3xl text-white">
                  10,492 <span className="text-base font-normal text-white/45">active nodes</span>
                </p>
              </div>
              <span className="pill">Search region or node ID</span>
            </div>
            <div className="glass-inset relative h-[480px] overflow-hidden rounded-3xl">
              <Globe />
            </div>
          </section>

          <div className="flex flex-col gap-8">
            <section className="glass flex flex-1 flex-col justify-center p-7 md:p-8">
              <p className="label">Replenishment efficiency</p>
              <p className="stat-num mt-6 text-7xl text-white">
                84<span className="text-4xl text-white/40">%</span>
              </p>
              <p className="mt-5 inline-flex items-center gap-2 text-sm text-emerald-300">
                <span className="h-2 w-2 rounded-full bg-emerald-400" />
                Solar index nominal
              </p>
            </section>

            <section className="glass p-7 md:p-8">
              <p className="label mb-6">ML algorithm state</p>
              <pre className="glass-inset whitespace-pre-wrap p-5 font-mono text-[0.8rem] leading-7 text-emerald-300/90">
                {algoState}
              </pre>
            </section>
          </div>
        </motion.div>
      </motion.div>
    </div>
  );
}
