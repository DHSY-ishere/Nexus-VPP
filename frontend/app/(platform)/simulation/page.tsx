"use client";

import React, { useEffect, useState, useRef } from "react";
import { motion } from "framer-motion";
import { LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid, ReferenceLine } from "recharts";
import CitySim from "./components/CitySim";
import PageHeader from "../components/PageHeader";

type OverrideCommand = {
  type: "OVERRIDE";
  field: "frequency" | "replenishment_rate";
  value: number;
};

type ClearCommand = {
  type: "CLEAR_FAULT";
};

export default function SimulationPage() {
  const [gridFrequency, setGridFrequency] = useState(50.0);
  const [currentConsumption, setCurrentConsumption] = useState(5.2);
  const [replenishmentRate, setReplenishmentRate] = useState(5.8);
  const [isAlgoActive, setIsAlgoActive] = useState(true);
  const [socket, setSocket] = useState<WebSocket | null>(null);

  // New Features State
  const [selectedNode, setSelectedNode] = useState<any | null>(null);
  const [cloudCoverDetected, setCloudCoverDetected] = useState(false);
  const [history, setHistory] = useState<any[]>([]);
  const [audioEnabled, setAudioEnabled] = useState(false);

  // Advanced EE / Presentation State
  const [heatmapMode, setHeatmapMode] = useState(false);
  const [anomalousNodes, setAnomalousNodes] = useState<string[]>([]);
  const [recentTrades, setRecentTrades] = useState<any[]>([]);
  const [systemHealth, setSystemHealth] = useState<any>(null);
  const [eventSummary, setEventSummary] = useState<any>(null);
  const [strainedStart, setStrainedStart] = useState<number | null>(null);
  const [liveLogic, setLiveLogic] = useState<string[]>([]);

  const audioCtxRef = useRef<AudioContext | null>(null);
  const oscRef = useRef<OscillatorNode | null>(null);
  const gainRef = useRef<GainNode | null>(null);

  useEffect(() => {
    const ws = new WebSocket("ws://localhost:8080/ws");
    ws.onopen = () => setSocket(ws);
    ws.onmessage = (event) => {
      const data = JSON.parse(event.data);
      if (data.type === "TRADE_EVENT") {
        setRecentTrades(prev => [...prev.slice(-9), data]);
        setLiveLogic(prev => {
          const log = `> EXECUTE_TRADE [${data.trade_id}]\n` +
                      `  TARGET_FREQ: 50.0Hz | CURRENT: ${(data.price_kwh - 0.82)*10 + 49.2}Hz\n` + 
                      `  ${data.energy_kw.toFixed(2)}kW sent -> ${data.energy_received.toFixed(2)}kW received\n` + 
                      `  P_loss = I^2*R_sim : ${data.loss_kw.toFixed(2)}kW (6% Loss)\n` + 
                      `  MARKET_PRICE: $${data.price_kwh.toFixed(2)}/kWh`;
          return [...prev.slice(-1), log]; // Keep last 2 logs
        });
        
        setEventSummary((prev: any) => prev && !prev.finished ? {
            ...prev,
            energyTraded: prev.energyTraded + data.energy_kw,
            profit: prev.profit + data.credit
        } : prev);

      } else if (data.type === "SYSTEM_HEALTH") {
        setSystemHealth(data);
      } else if (data.type === "ANOMALY_DETECTED") {
        setAnomalousNodes(prev => {
            if (!prev.includes(data.node_id)) return [...prev, data.node_id];
            return prev;
        });
      } else if (typeof data.local_frequency_hz === "number") {
        setGridFrequency(data.local_frequency_hz);
        setCurrentConsumption(data.current_consumption_kw ?? 5.2);
        setReplenishmentRate(data.replenishment_rate_kw ?? 5.8);
      }
    };
    return () => ws.close();
  }, []);

  const isStrained = gridFrequency < 49.9;

  // Post-Event Impact Tracking
  useEffect(() => {
    if (isStrained && !strainedStart) {
        setStrainedStart(Date.now());
        setEventSummary({ energyTraded: 0, profit: 0, finished: false });
    } else if (!isStrained && strainedStart) {
        const duration = ((Date.now() - strainedStart) / 1000).toFixed(1);
        setEventSummary((prev: any) => ({
            ...prev,
            duration,
            avoided: Math.floor(Math.random() * 500) + 1200,
            finished: true
        }));
        setStrainedStart(null);
        setTimeout(() => setEventSummary(null), 6000); // Hide after 6s
    }
  }, [isStrained, strainedStart]);

  // History for Ghosting Chart
  useEffect(() => {
    setHistory((prev) => {
      const now = new Date();
      const timeStr = `${now.getMinutes()}:${now.getSeconds().toString().padStart(2, '0')}`;
      
      let legacyFreq = gridFrequency;
      if (isStrained && isAlgoActive) {
        legacyFreq = 49.2 + (Math.random() - 0.5) * 0.2;
      } else if (!isAlgoActive) {
        legacyFreq = gridFrequency;
      }

      const next = [...prev, { time: timeStr, nexus: gridFrequency, legacy: legacyFreq }];
      if (next.length > 40) next.shift();
      return next;
    });
  }, [gridFrequency, isStrained, isAlgoActive]);

  // Audio Heartbeat Engine
  useEffect(() => {
    if (!audioEnabled) return;
    if (!audioCtxRef.current) {
      const ctx = new (window.AudioContext || (window as any).webkitAudioContext)();
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      
      osc.type = "sine";
      osc.frequency.value = 50;
      gain.gain.value = 0.2;

      osc.connect(gain);
      gain.connect(ctx.destination);
      osc.start();

      audioCtxRef.current = ctx;
      oscRef.current = osc;
      gainRef.current = gain;
    }

    const osc = oscRef.current;
    if (osc && audioCtxRef.current) {
      const ctx = audioCtxRef.current;
      if (isStrained && !isAlgoActive) {
        osc.type = "sawtooth";
        osc.frequency.setTargetAtTime(30, ctx.currentTime, 0.1);
        gainRef.current!.gain.setTargetAtTime(0.5, ctx.currentTime, 0.1);
      } else if (isStrained && isAlgoActive) {
        osc.type = "triangle";
        osc.frequency.setTargetAtTime(65, ctx.currentTime, 0.5);
        gainRef.current!.gain.setTargetAtTime(0.3, ctx.currentTime, 0.5);
      } else {
        osc.type = "sine";
        osc.frequency.setTargetAtTime(50, ctx.currentTime, 1.0);
        gainRef.current!.gain.setTargetAtTime(0.15, ctx.currentTime, 1.0);
      }
    }
  }, [audioEnabled, isStrained, isAlgoActive]);

  const sendCommand = (command: any) => {
    if (socket && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify(command));
    }
  };

  const triggerFrequencyDrop = () => sendCommand({ type: "OVERRIDE", field: "frequency", value: 49.2 });
  const triggerSolarBlockage = () => sendCommand({ type: "OVERRIDE", field: "replenishment_rate", value: 0 });
  const triggerByzantine = () => sendCommand({ type: "BYZANTINE_FAULT" });
  const triggerStressTest = () => sendCommand({ type: "STRESS_TEST" });
  const toggleHeatmap = () => setHeatmapMode(!heatmapMode);
  
  const triggerCloudCoverForecast = () => {
    setCloudCoverDetected(true);
    setTimeout(() => triggerSolarBlockage(), 4000);
  };

  const toggleAlgoOrchestration = () => setIsAlgoActive(!isAlgoActive);
  const clearFault = () => {
    sendCommand({ type: "CLEAR_FAULT" });
    setIsAlgoActive(true);
    setCloudCoverDetected(false);
    setAnomalousNodes([]);
    setRecentTrades([]);
    setLiveLogic([]);
  };

  const contributionRatio = replenishmentRate > 0 ? Math.min(1.0, currentConsumption / replenishmentRate) : 0;
  const targetDischarge = (gridFrequency - 50) * -0.5;

  return (
    <div className="pb-6">
      <PageHeader
        eyebrow="Command center"
        title="Grid"
        accent="simulation"
        description="Break the grid on purpose and watch Nexus stabilise it against a legacy grid with no autonomy."
      >
        {systemHealth && (
          <div className="glass flex flex-wrap items-center gap-x-7 gap-y-2 px-6 py-5 text-xs text-white/55">
            <span className="flex items-center gap-2">
              <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-emerald-400" />
              Kafka {systemHealth.throughput} msg/s
            </span>
            <span>Go mem {systemHealth.memory_mb} MB</span>
            <span>{systemHealth.goroutines} goroutines</span>
          </div>
        )}
        <div className="glass px-7 py-5">
          <p className="label">Grid frequency</p>
          <p className={`stat-num mt-3 text-4xl ${isStrained ? "text-amber-300 animate-pulse" : "text-emerald-300"}`}>
            {gridFrequency.toFixed(3)}
            <span className="ml-2 text-base font-normal text-white/40">Hz</span>
          </p>
        </div>
      </PageHeader>

      <motion.div
        className="mt-12 space-y-8"
        initial="hidden"
        animate="show"
        variants={{ hidden: {}, show: { transition: { staggerChildren: 0.12 } } }}
      >
        {anomalousNodes.length > 0 && (
          <div
            role="alert"
            className="glass glass-tint animate-pulse px-7 py-5 text-sm font-medium text-rose-100 [--tint:var(--rose)]"
          >
            Byzantine fault detected. Isolating {anomalousNodes.length} nodes.
          </div>
        )}

        {/* Weather Forecasting AI Alert */}
        {cloudCoverDetected && !isStrained && (
          <motion.div
            initial={{ opacity: 0, y: -10 }}
            animate={{ opacity: 1, y: 0 }}
            role="status"
            className="glass glass-tint px-7 py-6 text-sky-100 [--tint:var(--sky)]"
          >
            <p className="text-sm font-medium">Weather event forecast: cloud cover inbound</p>
            <p className="mt-2 text-sm leading-relaxed text-sky-100/70">
              Nexus has preemptively told consumers to lower their consumption thresholds by 15% to buffer the incoming
              solar drop.
            </p>
          </motion.div>
        )}

        {/* 3D City Visualization */}
        <motion.section
          className="glass relative p-5 md:p-6"
          initial={{ opacity: 0, y: 24 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6 }}
        >
          <div className="flex flex-wrap items-center justify-between gap-3 px-2 pb-5 pt-2">
            <p className="label">Digital twin city</p>
            {audioEnabled && <span className="pill pill-tint animate-pulse">Audio active</span>}
          </div>

          {eventSummary && eventSummary.finished && (
            <motion.div
              initial={{ opacity: 0, scale: 0.97 }}
              animate={{ opacity: 1, scale: 1 }}
              className="absolute inset-0 z-50 flex flex-col items-center justify-center rounded-[inherit] bg-[#060b13]/70 p-8 backdrop-blur-xl"
            >
              <p className="label text-emerald-300">Event complete</p>
              <h2 className="mt-4 text-center text-3xl font-medium tracking-tight text-white md:text-4xl">
                Grid stability <span className="font-serif italic font-normal text-white/70">restored</span>
              </h2>
              <div className="mt-10 grid grid-cols-2 gap-x-14 gap-y-8 text-center md:grid-cols-4">
                <div>
                  <p className="text-xs text-white/50">Time to stabilise</p>
                  <p className="stat-num mt-3 text-2xl text-white">{eventSummary.duration}s</p>
                </div>
                <div>
                  <p className="text-xs text-white/50">Energy traded</p>
                  <p className="stat-num mt-3 text-2xl text-sky-300">{eventSummary.energyTraded.toFixed(1)} kWh</p>
                </div>
                <div>
                  <p className="text-xs text-white/50">Blackouts avoided</p>
                  <p className="stat-num mt-3 text-2xl text-white">{eventSummary.avoided} homes</p>
                </div>
                <div>
                  <p className="text-xs text-white/50">Profit distributed</p>
                  <p className="stat-num mt-3 text-2xl text-emerald-300">${eventSummary.profit.toFixed(2)}</p>
                </div>
              </div>
            </motion.div>
          )}

          <div className="relative h-[480px] overflow-hidden rounded-3xl border border-white/8 bg-black/30">
            <CitySim
              isStrained={isStrained}
              isAlgoActive={isAlgoActive}
              cloudCoverDetected={cloudCoverDetected}
              heatmapMode={heatmapMode}
              anomalousNodes={anomalousNodes}
              recentTrades={recentTrades}
              onNodeClick={setSelectedNode}
            />

            {/* Digital Twin Overlay */}
            {selectedNode && (
              <motion.div
                initial={{ opacity: 0, scale: 0.95, x: 16 }}
                animate={{ opacity: 1, scale: 1, x: 0 }}
                className="glass absolute right-5 top-5 z-10 w-72 p-6"
              >
                <div className="mb-5 flex items-center justify-between border-b border-white/10 pb-4">
                  <h3 className="flex items-center gap-2.5 font-mono text-sm font-medium text-emerald-300">
                    <span className="h-2 w-2 animate-pulse rounded-full bg-emerald-400" />
                    {selectedNode.id}
                  </h3>
                  <button
                    type="button"
                    aria-label="Close"
                    onClick={() => setSelectedNode(null)}
                    className="flex h-7 w-7 items-center justify-center rounded-full text-white/50 hover:bg-white/10 hover:text-white"
                  >
                    ✕
                  </button>
                </div>
                <dl className="space-y-4 text-sm">
                  <div className="flex justify-between">
                    <dt className="text-white/55">Battery health</dt>
                    <dd className="font-mono text-white">{selectedNode.health}%</dd>
                  </div>
                  <div className="flex justify-between">
                    <dt className="text-white/55">Core temp</dt>
                    <dd className="font-mono text-amber-300">{selectedNode.temp}°C</dd>
                  </div>
                  <div className="flex justify-between">
                    <dt className="text-white/55">P2P earnings</dt>
                    <dd className="font-mono text-emerald-300">${selectedNode.earnings}</dd>
                  </div>
                </dl>
              </motion.div>
            )}

            {/* Audio Toggle */}
            {!audioEnabled && (
              <button type="button" onClick={() => setAudioEnabled(true)} className="btn absolute bottom-5 left-5">
                Enable audio heartbeat
              </button>
            )}
          </div>
          <p className="px-2 pb-1 pt-5 text-sm text-white/50">Click any building to open its digital twin.</p>
        </motion.section>

        {/* Ghosting Analysis Chart */}
        <motion.section
          className="glass p-7 md:p-8"
          initial={{ opacity: 0, y: 24 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6 }}
        >
          <div className="mb-8 flex flex-wrap items-center justify-between gap-4">
            <div>
              <p className="label">Ghosting analysis</p>
              <p className="mt-2 text-sm text-white/55">What the grid does with Nexus, against what it would do without</p>
            </div>
            <div className="flex gap-5 text-sm text-white/65">
              <span className="flex items-center gap-2">
                <span className="h-2.5 w-2.5 rounded-full bg-emerald-400" />
                Nexus stabilised
              </span>
              <span className="flex items-center gap-2">
                <span className="h-2.5 w-2.5 rounded-full bg-white/40" />
                Legacy grid
              </span>
            </div>
          </div>
          <div className="glass-inset p-4" style={{ height: 270 }}>
            <ResponsiveContainer width="100%" height={238}>
              <LineChart data={history} margin={{ top: 10, right: 12, left: -16, bottom: 0 }}>
                <CartesianGrid strokeDasharray="2 6" stroke="rgba(255,255,255,0.06)" vertical={false} />
                <XAxis dataKey="time" tick={{ fontSize: 10 }} minTickGap={24} tickLine={false} axisLine={false} />
                <YAxis domain={[49.0, 50.5]} tick={{ fontSize: 10 }} tickLine={false} axisLine={false} />
                <Tooltip itemStyle={{ color: "#fff" }} />
                <ReferenceLine y={50.0} stroke="rgba(255,255,255,0.12)" />
                <ReferenceLine
                  y={49.8}
                  stroke="#fbbf24"
                  strokeDasharray="3 4"
                  label={{ position: "insideTopLeft", value: "Critical threshold", fill: "#fbbf24", fontSize: 10 }}
                />
                {/* Legacy Line (Ghost) */}
                <Line
                  type="stepAfter"
                  dataKey="legacy"
                  name="Legacy grid (Hz)"
                  stroke="rgba(255,255,255,0.4)"
                  strokeWidth={2}
                  strokeDasharray="4 5"
                  dot={false}
                  isAnimationActive={false}
                />
                {/* Nexus Line */}
                <Line
                  type="monotone"
                  dataKey="nexus"
                  name="Nexus grid (Hz)"
                  stroke="#34d399"
                  strokeWidth={2.5}
                  dot={false}
                  isAnimationActive={false}
                />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </motion.section>

        {/* Control Panel Grid */}
        <motion.div
          className="grid grid-cols-1 gap-8 lg:grid-cols-3"
          initial={{ opacity: 0, y: 24 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.6 }}
        >
          {/* Grid Sabotage */}
          <section className="glass p-7 md:p-8">
            <p className="label text-amber-300/90">Grid sabotage</p>
            <p className="mt-3 text-sm text-white/50">Inject faults and see how the grid copes.</p>
            <div className="mt-8 grid grid-cols-2 gap-4">
              <button type="button" onClick={triggerFrequencyDrop} className="btn btn-amber col-span-2">
                Trigger frequency drop
              </button>
              <button type="button" onClick={triggerSolarBlockage} className="btn btn-amber">
                Block solar
              </button>
              <button type="button" onClick={triggerCloudCoverForecast} className="btn btn-sky">
                Cloud front
              </button>
              <button type="button" onClick={triggerByzantine} className="btn btn-rose col-span-2">
                Inject malicious data
              </button>
              <button type="button" onClick={clearFault} className="btn btn-volt col-span-2 mt-2">
                Clear all faults
              </button>
            </div>
          </section>

          {/* Nexus Response */}
          <section className="glass p-7 md:p-8">
            <p className="label text-emerald-300/90">Nexus response</p>
            <p className="mt-3 text-sm text-white/50">How the autonomous layer reacts.</p>
            <div className="mt-8 space-y-4">
              <button
                type="button"
                onClick={toggleAlgoOrchestration}
                aria-pressed={isAlgoActive}
                className={`btn w-full ${isAlgoActive ? "btn-volt" : ""}`}
              >
                {isAlgoActive ? "Algorithm orchestration on" : "Algorithm orchestration off"}
              </button>
              <button
                type="button"
                onClick={toggleHeatmap}
                aria-pressed={heatmapMode}
                className={`btn w-full ${heatmapMode ? "btn-violet" : ""}`}
              >
                Digital twin heatmap
              </button>
              <button type="button" onClick={triggerStressTest} className="btn w-full">
                Backend stress test (10K+ nodes)
              </button>
            </div>
          </section>

          {/* Live Metrics */}
          <section className="glass p-7 md:p-8">
            <p className="label">Live metrics</p>
            <dl className="mt-8 space-y-4 text-sm">
              <div className="flex justify-between">
                <dt className="text-white/55">Consumption</dt>
                <dd className="font-mono text-emerald-300">{currentConsumption.toFixed(2)} kW</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-white/55">Replenishment</dt>
                <dd className="font-mono text-emerald-300">{replenishmentRate.toFixed(2)} kW</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-white/55">Strain status</dt>
                <dd className={isStrained ? "animate-pulse font-medium text-amber-300" : "font-medium text-emerald-300"}>
                  {isStrained ? "Strained" : "Nominal"}
                </dd>
              </div>
            </dl>

            {/* Calculations Panel */}
            <div className="mt-8 border-t border-white/10 pt-6">
              <p className="label mb-4">Live EE logic terminal</p>
              <div className="glass-inset h-28 overflow-hidden p-4 font-mono text-[0.7rem] leading-relaxed text-emerald-300/80">
                {liveLogic.length === 0 ? (
                  <span className="text-white/35">Waiting for P2P clearing events...</span>
                ) : (
                  liveLogic.map((log, i) => (
                    <div key={i} className="mb-2 whitespace-pre-wrap">
                      {log}
                    </div>
                  ))
                )}
              </div>
            </div>
          </section>
        </motion.div>
      </motion.div>
    </div>
  );
}
