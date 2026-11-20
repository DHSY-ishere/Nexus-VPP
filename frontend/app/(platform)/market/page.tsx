"use client";

import React, { useCallback, useEffect, useRef, useState } from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import PageHeader from "../components/PageHeader";

type Trade = {
  trade_id: string;
  zone: string;
  seller_id: string;
  buyer_id: string;
  energy_kwh: number;
  price: number;
  loss_factor: number;
  gate_result: "PASS" | "REROUTED" | "CURTAILED";
  status: string;
  stability_credit: number;
  levy: number;
  fill: number;
  charged: number;
};

type Zone = {
  zone: string;
  price: number;
  auction_kwh: number;
  cleared_kwh: number;
  gated_kwh: number;
  trades: number;
  stress_before: number;
  stress_after: number;
  peak_load_pct: number;
  min_pu: number;
  max_pu: number;
  gate_mode: "LEARNED" | "WARMING_UP" | "MODEL";
  buses_learned: number;
  buses: number;
  sens_err_pct: number;
  model_cleared_kwh: number;
  conserved: boolean;
  excess_kwh: number;
  tolerance_kwh: number;
  loss_kwh: number;
  claimed_usd: number;
  confirmed_usd: number;
};

type Interval = {
  type: string;
  interval: string;
  mode: "GRID_CONNECTED" | "ISLANDED";
  cloud: number;
  zones: Zone[];
  trades: Trade[] | null;
  merkle_root: string;
  disputed: number;
  stress_removed: number;
  reflex_kw: number;
  pool_balance: number;
  community_savings: number;
  gini: number;
  chain_on: boolean;
  block_height: number;
  block_hash: string;
  block_sigs: number;
  quorum: number;
  validators: number;
  proposer: string;
  chain_err: string;
};

type Audit = {
  valid: boolean;
  blocks: number;
  height: number;
  quorum: number;
  validators: number;
  audit_ms: number;
  error?: string;
};

type Receipt = {
  verdict: string;
  outcome: { fill: number; status: string; charge: number; escrow: number };
  buyer: { baseline_kwh: number; reading_kwh: number; party: { traded_kwh: number; measured_kwh: number } };
  seller: { baseline_kwh: number; reading_kwh: number; party: { traded_kwh: number; measured_kwh: number } };
  zone: {
    zone: string;
    conserved: boolean;
    homes_net_kwh: number;
    loss_kwh: number;
    head_meter_kwh: number;
    excess_kwh: number;
    tolerance_kwh: number;
    note: string;
  };
  block: { height: number; hash: string; signatures: number; quorum: number; proposer: string };
};

type Proof = {
  trade_id: string;
  leaf: string;
  root: string;
  steps: { hash: string; left: boolean }[];
  valid: boolean;
};

const ZONE_COLORS: Record<string, string> = {
  Z1: "#34d399",
  Z2: "#60a5fa",
  Z3: "#fbbf24",
  Z4: "#c4b5fd",
};

const GATE_STYLE: Record<string, string> = {
  PASS: "text-emerald-300",
  REROUTED: "text-sky-300",
  CURTAILED: "text-amber-300",
};

const API = "http://localhost:8080";

export default function MarketPage() {
  const [latest, setLatest] = useState<Interval | null>(null);
  const [history, setHistory] = useState<Record<string, number | string>[]>([]);
  const [trades, setTrades] = useState<Trade[]>([]);
  const [connected, setConnected] = useState(false);
  const [proof, setProof] = useState<Proof | null>(null);
  const [proofError, setProofError] = useState<string | null>(null);
  const [totalGated, setTotalGated] = useState(0);
  const [audit, setAudit] = useState<Audit | null>(null);
  const [receipt, setReceipt] = useState<Receipt | null>(null);
  const socketRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    const socket = new WebSocket("ws://localhost:8080/ws");
    socketRef.current = socket;
    socket.onopen = () => setConnected(true);
    socket.onclose = () => setConnected(false);
    socket.onmessage = (event) => {
      const data = JSON.parse(event.data) as Interval;
      if (data.type !== "MARKET_INTERVAL") return;
      setLatest(data);
      const clock = new Date(data.interval).toISOString().slice(11, 16);
      const point: Record<string, number | string> = { t: clock };
      for (const z of data.zones) if (z.price > 0) point[z.zone] = Number(z.price.toFixed(3));
      setHistory((prev) => [...prev, point].slice(-72));
      setTotalGated((prev) => prev + data.zones.reduce((s, z) => s + z.gated_kwh, 0));
      const incoming = data.trades ?? [];
      if (incoming.length > 0) setTrades((prev) => [...incoming, ...prev].slice(0, 14));
    };
    return () => socket.close();
  }, []);

  const send = useCallback((msg: Record<string, unknown>) => {
    const s = socketRef.current;
    if (s && s.readyState === WebSocket.OPEN) s.send(JSON.stringify(msg));
  }, []);

  const checkProof = async (tradeId: string) => {
    setProofError(null);
    setProof(null);
    setReceipt(null);
    try {
      fetch(`${API}/ledger/delivery?trade_id=${encodeURIComponent(tradeId)}`)
        .then((r) => (r.ok ? r.json() : null))
        .then((r) => r && setReceipt(r as Receipt))
        .catch(() => undefined);
      const res = await fetch(`${API}/market/proof?trade_id=${encodeURIComponent(tradeId)}`);
      if (!res.ok) {
        setProofError("Proof no longer retained (the server keeps the last two hours).");
        return;
      }
      setProof((await res.json()) as Proof);
    } catch {
      setProofError("Could not reach the market server.");
    }
  };

  const runAudit = async () => {
    setAudit(null);
    try {
      const res = await fetch(`${API}/ledger/verify`);
      if (res.ok) setAudit((await res.json()) as Audit);
    } catch {
      setAudit({ valid: false, blocks: 0, height: 0, quorum: 0, validators: 0, audit_ms: 0, error: "Could not reach the server." });
    }
  };

  const islanded = latest?.mode === "ISLANDED";
  const clock = latest ? new Date(latest.interval).toISOString().slice(11, 16) : "--:--";
  const auctionTotal = latest?.zones.reduce((s, z) => s + z.auction_kwh, 0) ?? 0;
  const clearedTotal = latest?.zones.reduce((s, z) => s + z.cleared_kwh, 0) ?? 0;

  return (
    <div>
      <PageHeader
        eyebrow="Grid-aware market"
        title="P2P energy,"
        accent="cleared safely"
        description="A 5-minute auction sets one price per feeder. A network gate then cuts any trade the cables can't carry."
      >
        <div className="glass px-6 py-5">
          <p className="label">Simulated time</p>
          <p className="stat-num mt-3 font-mono text-2xl text-white">
            {clock} <span className="text-sm font-normal text-white/40">UTC</span>
          </p>
        </div>
        <div className="glass flex items-center gap-4 px-6 py-5">
          <span
            className={`pill pill-tint ${islanded ? "[--tint:var(--rose)]" : ""}`}
          >
            {latest ? (islanded ? "Islanded" : "Grid connected") : "Waiting"}
          </span>
          <span className={`flex items-center gap-2 text-sm ${connected ? "text-emerald-300" : "text-rose-300"}`}>
            <span className={`h-2 w-2 rounded-full ${connected ? "bg-emerald-400 animate-pulse" : "bg-rose-400"}`} />
            {connected ? "Live" : "Offline"}
          </span>
        </div>
      </PageHeader>

      {!connected && (
        <p role="status" className="glass mt-8 px-6 py-4 text-sm text-white/60">
          Not connected. Start the backend (<span className="font-mono">go run ./cmd/marketdemo</span> in{" "}
          <span className="font-mono">backend/</span>) and this page will fill in on its own.
        </p>
      )}

      <section className="glass mt-12 p-7 md:p-8" aria-label="Scenario controls">
        <p className="label">Scenarios</p>
        <div className="mt-6 flex flex-wrap gap-4">
          <button
            type="button"
            onClick={() => send({ type: "MARKET_ISLAND", value: !islanded })}
            aria-pressed={islanded}
            className={`btn ${islanded ? "btn-rose" : ""}`}
          >
            {islanded ? "Reconnect to main grid" : "Cut main grid (island mode)"}
          </button>
          <button
            type="button"
            onClick={() => send({ type: "MARKET_CLOUD", value: latest && latest.cloud < 0.5 ? 1 : 0.15 })}
            className={`btn ${latest && latest.cloud < 0.5 ? "btn-sky" : ""}`}
          >
            {latest && latest.cloud < 0.5 ? "Clear the clouds" : "Cloud front over the city"}
          </button>
          <button type="button" onClick={() => send({ type: "MARKET_TAMPER", value: true })} className="btn btn-amber">
            Make a seller under-deliver
          </button>
          <button type="button" onClick={() => send({ type: "MARKET_METERLIE", value: true })} className="btn btn-rose">
            Seller&apos;s meter lies about delivery
          </button>
          <button type="button" onClick={() => send({ type: "CLEAR_FAULT" })} className="btn btn-volt">
            Reset all
          </button>
        </div>
      </section>

      <section className="mt-8 grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-5" aria-label="Interval summary">
        <Stat label="Auction volume" value={`${auctionTotal.toFixed(2)}`} unit="kWh" />
        <Stat
          label="Cleared after gate"
          value={`${clearedTotal.toFixed(2)}`}
          unit="kWh"
          note={`${totalGated.toFixed(1)} kWh blocked since load`}
        />
        <Stat
          label="Stress removed"
          value={latest ? latest.stress_removed.toFixed(3) : "0"}
          note="against the no-help case"
        />
        <Stat
          label="Fairness (Gini)"
          value={latest ? latest.gini.toFixed(2) : "0"}
          note={`community saved $${(latest?.community_savings ?? 0).toFixed(2)}`}
        />
        <Stat
          label="Solidarity pool"
          value={`$${(latest?.pool_balance ?? 0).toFixed(2)}`}
          note={`${latest?.disputed ?? 0} disputed this interval`}
        />
      </section>

      <section className="mt-8 space-y-8">
        <div className="glass p-7 md:p-8">
          <div className="mb-8 flex flex-wrap items-center justify-between gap-4">
            <div>
              <p className="label">Clearing price per zone</p>
              <p className="mt-2 text-sm text-white/55">
                One price per zone and interval. Zones diverge when the gate keeps cheap energy from a congested feeder.
              </p>
            </div>
            <div className="flex flex-wrap gap-4 text-sm text-white/65">
              {Object.entries(ZONE_COLORS).map(([z, c]) => (
                <span key={z} className="flex items-center gap-2">
                  <span className="h-2.5 w-2.5 rounded-full" style={{ background: c }} />
                  {z}
                </span>
              ))}
            </div>
          </div>
          <div className="glass-inset p-4" style={{ height: 320 }}>
            <ResponsiveContainer width="100%" height={288}>
              <LineChart data={history} margin={{ top: 8, right: 12, left: -8, bottom: 0 }}>
                <CartesianGrid stroke="rgba(255,255,255,0.06)" strokeDasharray="2 6" vertical={false} />
                <XAxis dataKey="t" tick={{ fontSize: 10 }} tickLine={false} axisLine={false} minTickGap={28} />
                <YAxis tick={{ fontSize: 10 }} tickLine={false} axisLine={false} domain={[0, "auto"]} />
                <Tooltip />
                {Object.entries(ZONE_COLORS).map(([z, c]) => (
                  <Line
                    key={z}
                    type="stepAfter"
                    dataKey={z}
                    stroke={c}
                    strokeWidth={2}
                    dot={false}
                    isAnimationActive={false}
                    connectNulls
                  />
                ))}
              </LineChart>
            </ResponsiveContainer>
          </div>
        </div>

        <div className="glass p-7 md:p-8">
          <p className="label">Feeders</p>
          <p className="mt-3 max-w-2xl text-sm leading-relaxed text-white/50">
            The gate has no cable model. Homes nudge their battery power and measure their own voltage; the market
            rebuilds the feeder from those readings.
          </p>
          <ul className="mt-8 grid grid-cols-1 gap-6 sm:grid-cols-2 xl:grid-cols-4">
            {(latest?.zones ?? []).map((z) => {
              const hot = z.peak_load_pct > 90 || z.max_pu > 1.05 || z.min_pu < 0.95;
              return (
                <li key={z.zone} className="glass-inset p-5">
                  <div className="flex items-center justify-between">
                    <span className="flex items-center gap-2.5 font-medium text-white">
                      <span className="h-2.5 w-2.5 rounded-full" style={{ background: ZONE_COLORS[z.zone] }} />
                      {z.zone}
                    </span>
                    <span className={`pill ${hot ? "pill-tint [--tint:var(--amber)]" : "pill-tint"}`}>
                      {hot ? "Near limit" : "Within limits"}
                    </span>
                  </div>
                  <div className="mt-4 h-1.5 overflow-hidden rounded-full bg-white/10">
                    <div
                      className={`h-full rounded-full ${hot ? "bg-amber-400" : "bg-emerald-400"}`}
                      style={{ width: `${Math.min(100, z.peak_load_pct)}%` }}
                    />
                  </div>
                  <dl className="mt-4 grid grid-cols-2 gap-x-4 gap-y-2 text-xs text-white/55">
                    <dt>Cable load</dt>
                    <dd className="text-right font-mono text-white/80">{z.peak_load_pct.toFixed(0)}%</dd>
                    <dt>Voltage</dt>
                    <dd className="text-right font-mono text-white/80">
                      {(z.min_pu * 230).toFixed(0)}–{(z.max_pu * 230).toFixed(0)} V
                    </dd>
                    <dt>Cleared</dt>
                    <dd className="text-right font-mono text-white/80">{z.cleared_kwh.toFixed(2)} kWh</dd>
                    <dt>Gated</dt>
                    <dd className={`text-right font-mono ${z.gated_kwh > 0.001 ? "text-amber-300" : "text-white/80"}`}>
                      {z.gated_kwh.toFixed(2)} kWh
                    </dd>
                  </dl>
                  <div className="mt-4 flex flex-wrap items-center justify-between gap-2 border-t border-white/8 pt-4 text-xs">
                    <span
                      className={
                        z.gate_mode === "LEARNED"
                          ? "text-emerald-300"
                          : z.gate_mode === "MODEL"
                            ? "text-white/60"
                            : "text-amber-300"
                      }
                    >
                      {z.gate_mode === "WARMING_UP" ? "Warming up (thermal only)" : `Gate ${z.gate_mode.toLowerCase()}`}
                    </span>
                    <span className="text-white/45">
                      {z.buses_learned}/{z.buses} homes probed
                    </span>
                    <span className={`w-full ${z.conserved ? "text-emerald-300/80" : "text-rose-300"}`}>
                      {z.conserved
                        ? `Meters match the feeder-head meter (off ${z.excess_kwh.toFixed(3)} kWh, allowed ±${z.tolerance_kwh.toFixed(3)})`
                        : `Meters do NOT match the head meter: ${z.excess_kwh.toFixed(3)} kWh unconfirmed. Only $${z.confirmed_usd.toFixed(3)} of $${z.claimed_usd.toFixed(3)} released.`}
                    </span>
                    <span className="w-full text-white/40">
                      {z.model_cleared_kwh > 0.001
                        ? `${((z.cleared_kwh / z.model_cleared_kwh) * 100).toFixed(0)}% of perfect-model volume · ${z.sens_err_pct.toFixed(1)}% error`
                        : `Sensitivity error ${z.sens_err_pct.toFixed(1)}%`}
                    </span>
                  </div>
                </li>
              );
            })}
          </ul>
        </div>
      </section>

      <section className="glass mt-8 p-7 md:p-8" aria-label="Settlement blockchain">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <p className="label">Settlement blockchain</p>
            <p className="mt-3 max-w-3xl text-sm leading-relaxed text-white/50">
              Power can&apos;t be routed to one buyer, so delivery is checked for the whole feeder: every home&apos;s signed
              meter plus an independent feeder-head meter must add up. Each interval is one block. Every validator
              re-runs the settlement rules itself and signs only if its answer is identical; a block needs{" "}
              {latest?.quorum ?? 3} of {latest?.validators ?? 4} signatures.
            </p>
          </div>
          <button type="button" onClick={runAudit} className="btn">
            Audit the whole chain
          </button>
        </div>
        <dl className="mt-8 grid grid-cols-2 gap-6 text-sm sm:grid-cols-4">
          <div>
            <dt className="label">Block height</dt>
            <dd className="stat-num mt-2 text-2xl text-white">{latest?.chain_on ? latest.block_height : "–"}</dd>
          </div>
          <div>
            <dt className="label">Signatures</dt>
            <dd className="stat-num mt-2 text-2xl text-white">
              {latest?.chain_on ? `${latest.block_sigs} / ${latest.validators}` : "–"}
            </dd>
          </div>
          <div>
            <dt className="label">Proposer</dt>
            <dd className="stat-num mt-2 text-2xl text-white">{latest?.proposer || "–"}</dd>
          </div>
          <div className="col-span-2 sm:col-span-1">
            <dt className="label">Block hash</dt>
            <dd className="mt-2 break-all font-mono text-xs text-white/55">
              {latest?.block_hash ? `${latest.block_hash.slice(0, 32)}…` : "–"}
            </dd>
          </div>
        </dl>
        {latest?.chain_err && (
          <p className="mt-6 text-sm text-rose-300" role="alert">
            Consensus failed, nothing settled this interval: {latest.chain_err}
          </p>
        )}
        {audit && (
          <p className={`mt-6 text-sm ${audit.valid ? "text-emerald-300" : "text-rose-300"}`} role="status">
            {audit.valid
              ? `Audit passed: ${audit.blocks} blocks re-executed and verified in ${audit.audit_ms.toFixed(0)} ms (hash links, ${audit.quorum}-of-${audit.validators} signatures, settlement outcomes).`
              : `Audit FAILED: ${audit.error}`}
          </p>
        )}
      </section>

      <section className="glass mt-8 p-7 md:p-8">
        <div className="mb-8 flex flex-wrap items-baseline justify-between gap-3">
          <p className="label">Settled trades</p>
          <span className="max-w-full truncate font-mono text-xs text-white/40">
            interval root {latest ? latest.merkle_root.slice(0, 24) : "-"}…
          </span>
        </div>
        <div className="overflow-x-auto">
          <table className="table-glass min-w-[860px]">
            <thead>
              <tr>
                <th>Trade</th>
                <th>Seller → Buyer</th>
                <th>kWh</th>
                <th>$/kWh</th>
                <th>Gate</th>
                <th>Status</th>
                <th>Stability credit</th>
                <th>Proof</th>
              </tr>
            </thead>
            <tbody>
              {trades.length === 0 && (
                <tr>
                  <td colSpan={8} className="py-10 text-center text-white/45">
                    No trades yet. Sellers appear once the solar day has filled the batteries.
                  </td>
                </tr>
              )}
              {trades.map((t) => (
                <tr key={t.trade_id}>
                  <td className="font-mono text-white/60">{t.trade_id}</td>
                  <td>
                    {t.seller_id} <span className="text-white/35">→</span> {t.buyer_id}
                  </td>
                  <td className="font-mono">{t.energy_kwh.toFixed(3)}</td>
                  <td className="font-mono">{t.price.toFixed(3)}</td>
                  <td className={GATE_STYLE[t.gate_result] ?? ""}>{t.gate_result.toLowerCase()}</td>
                  <td
                    className={
                      t.status === "DISPUTED" ? "text-rose-300" : t.status === "PARTIAL" ? "text-amber-300" : "text-white/80"
                    }
                  >
                    {t.status.toLowerCase()}
                    {t.status !== "SETTLED" && <span className="ml-1 text-white/40">({(t.fill * 100).toFixed(0)}%)</span>}
                  </td>
                  <td className="font-mono">{t.stability_credit > 0 ? `$${t.stability_credit.toFixed(3)}` : "–"}</td>
                  <td>
                    <button
                      type="button"
                      onClick={() => checkProof(t.trade_id)}
                      className="text-emerald-300 underline decoration-emerald-300/40 underline-offset-4 hover:decoration-emerald-300"
                    >
                      Verify
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {receipt && (
          <div className="glass-inset mt-6 p-5" role="status" aria-label="Proof of receipt">
            <p className={`text-sm font-medium ${receipt.outcome.status === "SETTLED" ? "text-emerald-300" : "text-amber-300"}`}>
              {receipt.verdict}
            </p>
            <dl className="mt-4 grid grid-cols-1 gap-x-8 gap-y-2 text-xs text-white/55 sm:grid-cols-2">
              <dt>Buyer drew above baseline</dt>
              <dd className="font-mono text-white/80 sm:text-right">
                {receipt.buyer.party.measured_kwh.toFixed(3)} of {receipt.buyer.party.traded_kwh.toFixed(3)} kWh (signed meter)
              </dd>
              <dt>Seller injected above baseline</dt>
              <dd className="font-mono text-white/80 sm:text-right">
                {receipt.seller.party.measured_kwh.toFixed(3)} of {receipt.seller.party.traded_kwh.toFixed(3)} kWh (signed meter)
              </dd>
              <dt>All {receipt.zone.zone} home meters (net, minus cable losses)</dt>
              <dd className="font-mono text-white/80 sm:text-right">
                {(receipt.zone.homes_net_kwh - receipt.zone.loss_kwh).toFixed(3)} kWh
              </dd>
              <dt>Independent feeder-head meter</dt>
              <dd className="font-mono text-white/80 sm:text-right">{receipt.zone.head_meter_kwh.toFixed(3)} kWh</dd>
              <dt>Difference (allowed ±{receipt.zone.tolerance_kwh.toFixed(3)})</dt>
              <dd className={`font-mono sm:text-right ${receipt.zone.conserved ? "text-emerald-300" : "text-rose-300"}`}>
                {receipt.zone.excess_kwh.toFixed(3)} kWh
              </dd>
              <dt>Charged to buyer</dt>
              <dd className="font-mono text-white/80 sm:text-right">
                ${receipt.outcome.charge.toFixed(4)} of ${receipt.outcome.escrow.toFixed(4)}
              </dd>
            </dl>
            <p className="mt-4 break-all font-mono text-xs text-white/40">
              block {receipt.block.height} · {receipt.block.signatures}/{receipt.block.quorum}+ validator signatures · proposer{" "}
              {receipt.block.proposer} · {receipt.block.hash.slice(0, 32)}…
            </p>
          </div>
        )}

        {(proof || proofError) && (
          <div className="glass-inset mt-6 p-5" role="status">
            {proofError && <p className="text-sm text-amber-300">{proofError}</p>}
            {proof && (
              <>
                <p className={`text-sm font-medium ${proof.valid ? "text-emerald-300" : "text-rose-300"}`}>
                  {proof.valid ? "Verified" : "Invalid"}: {proof.trade_id} is {proof.valid ? "included" : "not included"}{" "}
                  in the published interval root.
                </p>
                <p className="mt-3 break-all font-mono text-xs leading-relaxed text-white/45">leaf {proof.leaf}</p>
                <p className="break-all font-mono text-xs leading-relaxed text-white/45">root {proof.root}</p>
                <p className="mt-1 text-xs text-white/45">{proof.steps.length} sibling hashes on the path</p>
              </>
            )}
          </div>
        )}
      </section>
    </div>
  );
}

function Stat({ label, value, unit, note }: { label: string; value: string; unit?: string; note?: string }) {
  return (
    <div className="glass p-6">
      <p className="label">{label}</p>
      <p className="stat-num mt-5 text-3xl text-white">
        {value}
        {unit && <span className="ml-1.5 text-sm font-normal text-white/40">{unit}</span>}
      </p>
      {note && <p className="mt-3 text-xs leading-relaxed text-white/45">{note}</p>}
    </div>
  );
}
