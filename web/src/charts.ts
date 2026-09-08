/** Chart builders — each returns a Chart the caller destroys before rebuild. */
import Chart from "chart.js/auto";
import type { RepoRow, TrendPoint, VerdictRow } from "./api";
import { fmtCost, fmtTokens } from "./format";

export const REPO_PALETTE = [
  "#2563eb", "#0ea5e9", "#10b981", "#f59e0b", "#8b5cf6",
  "#ec4899", "#14b8a6", "#f97316", "#6366f1", "#84cc16",
];

export const VERDICT_COLORS: Record<string, string> = {
  "CAN MERGE": "#22c55e",
  "CONDITIONAL MERGE": "#eab308",
  "CANNOT MERGE": "#ef4444",
  UNKNOWN: "#94a3b8",
  "": "#64748b", // 单评审模式
};

export function verdictLabel(v: string): string {
  return v === "" ? "单评审（无 verdict）" : v;
}

export type RepoMetric = "cost" | "input" | "output" | "cacheRead" | "tokens";

export function createRepoBar(
  canvas: HTMLCanvasElement,
  repos: RepoRow[],
  metric: RepoMetric,
): Chart {
  const value = (r: RepoRow): number =>
    metric === "cost" ? r.costTotal
    : metric === "input" ? r.input
    : metric === "output" ? r.output
    : metric === "cacheRead" ? r.cacheRead
    : r.input + r.output + r.cacheRead;
  return new Chart(canvas, {
    type: "bar",
    data: {
      labels: repos.map((r) => r.repository.split("/").pop() ?? r.repository),
      datasets: [{
        data: repos.map(value),
        backgroundColor: repos.map((_, i) => REPO_PALETTE[i % REPO_PALETTE.length]),
        borderRadius: 4,
        maxBarThickness: 48,
      }],
    },
    options: {
      indexAxis: "y",
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            label: (ctx) => {
              const r = repos[ctx.dataIndex];
              const v = value(r);
              return metric === "cost"
                ? `${fmtCost(v)} · ${r.reviews} 次评审`
                : `${fmtTokens(v)} tokens · ${r.reviews} 次评审`;
            },
          },
        },
      },
      scales: {
        x: { ticks: { callback: (v) => (metric === "cost" ? fmtCost(Number(v)) : fmtTokens(Number(v))) } },
      },
    },
  });
}

export function createVerdictDoughnut(
  canvas: HTMLCanvasElement,
  verdicts: VerdictRow[],
): Chart {
  return new Chart(canvas, {
    type: "doughnut",
    data: {
      labels: verdicts.map((v) => verdictLabel(v.verdict)),
      datasets: [{
        data: verdicts.map((v) => v.count),
        backgroundColor: verdicts.map((v) => VERDICT_COLORS[v.verdict] ?? "#94a3b8"),
        borderWidth: 2,
        borderColor: "#fff",
      }],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      cutout: "62%",
      plugins: { legend: { position: "right" } },
    },
  });
}

export type TrendMetric = "cost" | "tokens";

/** Stack daily cost/tokens per repository. Missing (repo, day) cells are gaps
 *  in the raw points; Chart.js handles sparse labels per dataset via nulls. */
export function createTrendLine(
  canvas: HTMLCanvasElement,
  points: TrendPoint[],
  metric: TrendMetric,
  repoColors: Map<string, string>,
): Chart {
  const days = [...new Set(points.map((p) => p.day))].sort();
  const repos = [...new Set(points.map((p) => p.repository))];
  const cell = new Map(points.map((p) => [`${p.day}|${p.repository}`, metric === "cost" ? p.cost : p.tokens]));
  return new Chart(canvas, {
    type: "line",
    data: {
      labels: days,
      datasets: repos.map((repo) => ({
        label: repo.split("/").pop() ?? repo,
        data: days.map((d) => cell.get(`${d}|${repo}`) ?? null),
        borderColor: repoColors.get(repo) ?? "#94a3b8",
        backgroundColor: (repoColors.get(repo) ?? "#94a3b8") + "33",
        fill: true,
        tension: 0.3,
        pointRadius: 2,
        spanGaps: true,
      })),
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      plugins: {
        legend: { position: "bottom" },
        tooltip: {
          callbacks: {
            label: (ctx) => {
              const v = ctx.parsed.y;
              const text = v === null ? "—" : metric === "cost" ? fmtCost(v) : fmtTokens(v);
              return `${ctx.dataset.label}: ${text}`;
            },
          },
        },
      },
      scales: { y: { ticks: { callback: (v) => (metric === "cost" ? fmtCost(Number(v)) : fmtTokens(Number(v))) }, beginAtZero: true } },
    },
  });
}
