import React from "react";

/**
 * Shared page header: small eyebrow, a title with one serif-italic accent word,
 * a one-line description, and a slot on the right for the page's headline number.
 */
export default function PageHeader({
  eyebrow,
  title,
  accent,
  description,
  children,
}: {
  eyebrow: string;
  title: string;
  accent: string;
  description?: string;
  children?: React.ReactNode;
}) {
  return (
    <header className="flex flex-col gap-6 md:flex-row md:items-end md:justify-between">
      <div className="max-w-2xl">
        <p className="label text-emerald-300/80">{eyebrow}</p>
        <h1 className="mt-3 text-4xl font-medium leading-[1.05] tracking-tight text-white md:text-5xl">
          {title}{" "}
          <span className="font-serif italic font-normal text-white/70">{accent}</span>
        </h1>
        {description && <p className="mt-4 text-base leading-relaxed text-white/60">{description}</p>}
      </div>
      {children && <div className="flex flex-wrap items-center gap-4 md:justify-end">{children}</div>}
    </header>
  );
}
