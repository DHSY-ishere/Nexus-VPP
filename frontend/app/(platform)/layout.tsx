"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

export default function PlatformLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  const pathname = usePathname();
  const isDashboard = pathname === "/dashboard";
  const isP2P = pathname === "/p2p";

  return (
    <div className="min-h-screen bg-[#050505] text-zinc-300 flex">
      <aside className="w-16 shrink-0 bg-[#0a0a0a] border-r border-zinc-900 flex flex-col items-center py-8 gap-8">
        <Link href="/dashboard" className="font-sans text-emerald-500 font-black text-lg tracking-tighter">
          N//
        </Link>
        <Link href="/dashboard" className={isDashboard ? "text-emerald-500" : "text-zinc-600"}>
          <svg className="h-6 w-6" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
            <path d="M3 3h8v8H3V3zm10 0h8v8h-8V3zM3 13h8v8H3v-8zm10 0h8v8h-8v-8z" />
          </svg>
        </Link>
        <Link href="/p2p" className={isP2P ? "text-emerald-500" : "text-zinc-600"}>
          <svg className="h-6 w-6" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
            <path d="M4 20h3V9H4v11zm6 0h3V4h-3v16zm6 0h3v-7h-3v7z" />
          </svg>
        </Link>
      </aside>
      <main className="flex-1 relative p-4 md:p-8 pb-16">{children}</main>
      <div className="crt-scanline pointer-events-none opacity-20 z-50 fixed inset-0" />
    </div>
  );
}
