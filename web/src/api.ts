/** API client + types mirroring the Go backend's JSON (internal/store). */

export interface Summary {
  reviews: number;
  input: number;
  output: number;
  cacheRead: number;
  costTotal: number;
  avgDurationS: number;
  cacheHitRate: number;
}

export interface RepoRow {
  repository: string;
  reviews: number;
  input: number;
  output: number;
  cacheRead: number;
  costTotal: number;
  lastTs: string;
}

export interface TrendPoint {
  day: string;
  repository: string;
  cost: number;
  tokens: number;
}

export interface VerdictRow {
  verdict: string; // "" = 单评审模式（无 coordinator verdict）
  count: number;
}

export interface RunRow {
  ts: string;
  platform: string;
  repository: string;
  pr: number;
  mode: string;
  verdict: string;
  severityDecision: string;
  blocking: number;
  warning: number;
  input: number;
  output: number;
  cacheRead: number;
  costTotal: number;
  durationS: number | null;
  personas: number;
}

export interface Dashboard {
  summary: Summary;
  repos: RepoRow[] | null;
  trend: TrendPoint[] | null;
  verdicts: VerdictRow[] | null;
  recent: RunRow[] | null;
}

export interface WindowState {
  days: number; // 0 = 全部
  repo: string; // "" = 全部仓库
}

export async function fetchDashboard(win: WindowState): Promise<Dashboard> {
  const params = new URLSearchParams({ days: String(win.days) });
  if (win.repo) params.set("repo", win.repo);
  const res = await fetch(`/api/dashboard?${params.toString()}`);
  if (!res.ok) throw new Error(`dashboard ${res.status}: ${await res.text()}`);
  return (await res.json()) as Dashboard;
}
