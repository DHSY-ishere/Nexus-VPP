"use client";

import React from "react";
import { motion } from "framer-motion";
import { useRouter } from "next/navigation";
import { Poppins, Source_Serif_4 } from "next/font/google";
import { ArrowRight, BookOpen, Download, Globe, Menu, Radio, Share2, Sparkles, Wand2 } from "lucide-react";

const poppins = Poppins({
  subsets: ["latin"],
  weight: ["400", "500", "600"],
});

const sourceSerif = Source_Serif_4({
  subsets: ["latin"],
  style: ["italic"],
  weight: ["400"],
});

export default function NexusDashboard() {
  const router = useRouter();

  return (
    <main className={`${poppins.className} min-h-screen bg-black relative overflow-hidden text-white`}>
      <video
        className="absolute inset-0 h-full w-full object-cover z-0"
        src="https://d8j0ntlcm91z4.cloudfront.net/user_38xzZboKViGWJOttwIXH07lWA1P/hf_20260315_073750_51473149-4350-4920-ae24-c8214286f323.mp4"
        autoPlay
        muted
        loop
        playsInline
      />
      <div className="absolute inset-0 bg-black/40 z-0" />

      <div className="relative z-10 min-h-screen">
        <div className="mx-auto w-full max-w-[1600px] min-h-screen px-4 py-4 lg:px-6 lg:py-6">
          <div className="min-h-[calc(100vh-2rem)] lg:min-h-[calc(100vh-3rem)] grid grid-cols-1 lg:grid-cols-[52%_48%] gap-4 lg:gap-6">
            <section className="min-h-0">
              <div className="liquid-glass-strong rounded-3xl h-full p-6 lg:p-8 flex flex-col">
                <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <div className="w-8 h-8 rounded-full liquid-glass flex items-center justify-center">
                  <Sparkles className="w-4 h-4" />
                </div>
                <p className="text-2xl font-semibold tracking-tighter text-white">nexus</p>
              </div>
                <button className="liquid-glass rounded-full px-4 py-2 text-sm text-white/80 hover:scale-105 active:scale-95 transition-transform inline-flex items-center gap-2">
                  <Menu className="w-4 h-4" />
                  Menu
                </button>
              </div>

              <motion.div
                className="flex-1 flex flex-col justify-center"
                initial="hidden"
                animate="show"
                variants={{ hidden: {}, show: { transition: { staggerChildren: 0.2 } } }}
              >
                <div className="mx-auto w-full max-w-3xl text-center lg:text-left">
                  <motion.div
                    className="w-20 h-20 rounded-full liquid-glass-strong flex items-center justify-center mb-8 mx-auto lg:mx-0"
                    initial={{ opacity: 0, y: 30 }}
                    animate={{ opacity: 1, y: 0 }}
                  >
                    <Sparkles className="w-9 h-9 text-white/80" />
                  </motion.div>
                  <motion.p
                    className="liquid-glass rounded-full px-4 py-2 text-xs text-white/80 mb-6 inline-flex"
                    initial={{ opacity: 0, y: 30 }}
                    animate={{ opacity: 1, y: 0 }}
                  >
                    NEXUS ORCHESTRATOR v3.1
                  </motion.p>
                  <motion.h1
                    className="text-5xl lg:text-7xl font-medium tracking-[-0.05em] leading-[0.95]"
                    initial={{ opacity: 0, y: 30 }}
                    animate={{ opacity: 1, y: 0 }}
                  >
                    Decentralizing the <br />
                    <span className={`${sourceSerif.className} italic text-white/80`}>current of Nexus VPP</span>
                  </motion.h1>
                  <motion.p
                    className="mt-6 text-white/60 max-w-xl mx-auto lg:mx-0"
                    initial={{ opacity: 0, y: 30 }}
                    animate={{ opacity: 1, y: 0 }}
                  >
                    The world&apos;s most advanced autonomous peer-to-peer virtual power plant control surface.
                  </motion.p>
                  <motion.button
                    type="button"
                    onClick={() => router.push("/dashboard")}
                    className="mt-10 liquid-glass-strong rounded-full px-6 py-3 text-white inline-flex items-center gap-3 hover:scale-105 active:scale-95 transition-transform"
                    initial={{ opacity: 0, y: 30 }}
                    animate={{ opacity: 1, y: 0 }}
                  >
                    INITIALIZE SESSION
                    <span className="w-7 h-7 rounded-full bg-white/15 flex items-center justify-center">
                      <Download className="w-4 h-4" />
                    </span>
                  </motion.button>
                  <motion.div
                    className="mt-8 flex flex-wrap gap-3 justify-center lg:justify-start"
                    initial={{ opacity: 0, y: 30 }}
                    animate={{ opacity: 1, y: 0 }}
                  >
                    {["Grid Observatory", "P2P Settlement", "Autonomous Dispatch"].map((pill) => (
                      <span key={pill} className="liquid-glass rounded-full px-4 py-2 text-xs text-white/80">
                        {pill}
                      </span>
                    ))}
                  </motion.div>
                </div>
              </motion.div>

              <div className="text-center lg:text-left mt-8">
                <p className="text-xs tracking-widest uppercase text-white/50">VISIONARY INFRASTRUCTURE</p>
                <p className="mt-3 text-white/80 text-lg">
                  We architected a <span className={`${sourceSerif.className} italic`}>grid with no boundaries.</span>
                </p>
                <div className="mt-3 flex items-center justify-center lg:justify-start gap-3 text-white/60 text-xs tracking-[0.2em]">
                  <span className="h-px w-10 bg-white/20" />
                  NEXUS SYSTEMS
                  <span className="h-px w-10 bg-white/20" />
                </div>
              </div>
            </div>
          </section>

          <section className="hidden lg:block min-h-0">
            <div className="h-full grid grid-rows-[auto_auto_1fr] gap-6">
              <div className="flex items-center justify-between">
                <div className="liquid-glass rounded-full px-4 py-2 inline-flex items-center gap-3">
                  {[Globe, Share2, Radio].map((Icon, idx) => (
                    <a key={idx} href="#" className="text-white hover:text-white/80 transition-colors hover:scale-105">
                      <Icon className="w-4 h-4" />
                    </a>
                  ))}
                  <ArrowRight className="w-4 h-4 text-white/80" />
                </div>
                <button className="liquid-glass rounded-full px-3 py-2 inline-flex items-center gap-2 text-white/80 hover:scale-105 transition-transform">
                  <span className="w-8 h-8 rounded-full bg-white/10 flex items-center justify-center">
                    <Sparkles className="w-4 h-4" />
                  </span>
                  Account
                </button>
              </div>

              <div className="liquid-glass rounded-3xl p-5 max-w-64">
                <h3 className="font-medium text-white">Join the Nexus network</h3>
                <p className="text-white/60 text-sm mt-2">Private access to autonomous orchestration workflows.</p>
              </div>

              <div className="self-end liquid-glass-strong rounded-[2.5rem] p-5 space-y-4">
                <div className="grid grid-cols-2 gap-4">
                  <div className="liquid-glass rounded-3xl p-4">
                    <Wand2 className="w-5 h-5 text-white/80 mb-2" />
                    <p className="text-white font-medium">Dispatch Engine</p>
                  </div>
                  <div className="liquid-glass rounded-3xl p-4">
                    <BookOpen className="w-5 h-5 text-white/80 mb-2" />
                    <p className="text-white font-medium">Grid Archive</p>
                  </div>
                </div>
                <div className="liquid-glass rounded-3xl p-4 flex items-center justify-between gap-4">
                  <div className="flex items-center gap-3 min-w-0">
                    <div className="w-24 h-16 rounded-xl bg-white/10 flex items-center justify-center shrink-0">
                      <Sparkles className="w-6 h-6 text-white/70" />
                    </div>
                    <div className="min-w-0">
                      <p className="font-medium text-white">Advanced Grid Sculpting</p>
                      <p className="text-sm text-white/60">Autonomous load-shaping and peer market balancing.</p>
                    </div>
                  </div>
                  <button className="w-8 h-8 rounded-full bg-white/10 text-white hover:scale-105 transition-transform shrink-0">
                    +
                  </button>
                </div>
              </div>
            </div>
          </section>
        </div>
      </div>
      </div>
    </main>
  );
}
