"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

type NavItem = { href: string; label: string; icon: React.ReactNode };

const icon = (d: string) => (
  <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
    <path d={d} />
  </svg>
);

const NAV: NavItem[] = [
  { href: "/dashboard", label: "Grid", icon: icon("M3 3h8v8H3V3zm10 0h8v8h-8V3zM3 13h8v8H3v-8zm10 0h8v8h-8v-8z") },
  { href: "/p2p", label: "P2P", icon: icon("M4 20h3V9H4v11zm6 0h3V4h-3v16zm6 0h3v-7h-3v7z") },
  { href: "/market", label: "Market", icon: icon("M13 2 4 14h6l-1 8 9-12h-6l1-8z") },
  {
    href: "/simulation",
    label: "Twin",
    icon: icon(
      "M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.41 0-8-3.59-8-8s3.59-8 8-8 8 3.59 8 8-3.59 8-8 8zm3.5-9c.83 0 1.5-.67 1.5-1.5S16.33 8 15.5 8 14 8.67 14 9.5s.67 1.5 1.5 1.5zm-7 0c.83 0 1.5-.67 1.5-1.5S9.33 8 8.5 8 7 8.67 7 9.5 7.67 11 8.5 11zm3.5 6.5c2.33 0 4.31-1.46 5.11-3.5H6.89c.8 2.04 2.78 3.5 5.11 3.5z",
    ),
  },
];

export default function PlatformLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  const pathname = usePathname();

  return (
    <div className="relative min-h-screen text-slate-200">
      <div className="ambient" aria-hidden="true">
        <i />
        <i />
        <i />
      </div>

      {/* Floating glass rail on desktop, floating dock on phones */}
      <nav
        aria-label="Primary"
        className="glass fixed z-40 flex items-center
          bottom-4 left-1/2 -translate-x-1/2 flex-row gap-1 rounded-full px-3 py-2
          lg:bottom-auto lg:left-6 lg:top-1/2 lg:-translate-y-1/2 lg:translate-x-0 lg:flex-col lg:gap-2 lg:rounded-[2rem] lg:px-3 lg:py-5"
      >
        <Link
          href="/"
          aria-label="Nexus home"
          className="hidden lg:flex mb-3 h-11 w-11 items-center justify-center rounded-2xl bg-white/5 border border-white/10 text-base font-semibold tracking-tight text-brand"
        >
          N
        </Link>
        {NAV.map((item) => {
          const active = pathname === item.href;
          return (
            <Link
              key={item.href}
              href={item.href}
              aria-current={active ? "page" : undefined}
              className={`flex w-[4.5rem] flex-col items-center gap-1.5 rounded-2xl px-2 py-3 text-[0.7rem] font-medium transition-colors ${
                active
                  ? "bg-emerald-400/15 text-emerald-300 shadow-[inset_0_1px_0_rgb(255_255_255/0.18)] border border-emerald-300/25"
                  : "border border-transparent text-white/55 hover:bg-white/8 hover:text-white"
              }`}
            >
              {item.icon}
              {item.label}
            </Link>
          );
        })}
      </nav>

      <main className="relative z-10 mx-auto w-full max-w-[1280px] px-5 pb-32 pt-10 sm:px-8 lg:pb-16 lg:pl-40 lg:pr-10 lg:pt-14">
        {children}
      </main>
    </div>
  );
}
