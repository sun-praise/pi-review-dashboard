/** Number formatting: tokens compact (k/M), costs adaptive, durations human. */

export function fmtTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

/** Costs run from ~$0.01 (single run) to hundreds (fleet total): keep enough
 *  decimals for the small values without drowning the big ones. */
export function fmtCost(usd: number): string {
  if (usd >= 100) return `$${usd.toFixed(1)}`;
  if (usd >= 1) return `$${usd.toFixed(2)}`;
  return `$${usd.toFixed(4)}`;
}

export function fmtPct(x: number): string {
  return `${(x * 100).toFixed(1)}%`;
}

export function fmtDuration(s: number | null): string {
  if (s === null) return "—";
  if (s < 90) return `${s.toFixed(0)}s`;
  if (s < 5400) return `${Math.floor(s / 60)}m${Math.round(s % 60)}s`;
  return `${(s / 3600).toFixed(1)}h`;
}

/** ISO timestamp → "MM-DD HH:mm" (UTC, matching the storage bucketing). */
export function fmtTs(iso: string): string {
  return iso.slice(5, 16).replace("T", " ");
}
