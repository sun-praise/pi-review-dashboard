import Chart from "chart.js/auto";
import { fetchDashboard, type Dashboard, type WindowState } from "./api";
import { fmtCost, fmtDuration, fmtPct, fmtTokens, fmtTs } from "./format";
import {
  createRepoBar, createTrendLine, createVerdictDoughnut,
  REPO_PALETTE, VERDICT_COLORS,
  type RepoMetric, type TrendMetric,
} from "./charts";

const state: WindowState = { days: 30, repo: "" };
let repoMetric: RepoMetric = "cost";
let trendMetric: TrendMetric = "cost";
let data: Dashboard | null = null;
const charts: Chart[] = [];
const repoColors = new Map<string, string>();

function $(id: string): HTMLElement {
  const el = document.getElementById(id);
  if (!el) throw new Error(`#${id} missing`);
  return el;
}

function renderCards(d: Dashboard): void {
  const s = d.summary;
  const cards: Array<[string, string, string?]> = [
    ["评审次数", String(s.reviews)],
    ["总成本", fmtCost(s.costTotal), "USD · 按 DeepSeek 估价"],
    ["Input tokens", fmtTokens(s.input), "缓存未命中的输入"],
    ["Output tokens", fmtTokens(s.output)],
    ["Cache Read", fmtTokens(s.cacheRead), `命中率 ${fmtPct(s.cacheHitRate)}`],
    ["缓存命中率", fmtPct(s.cacheHitRate), "cacheRead / (input+cacheRead)"],
    ["平均耗时", fmtDuration(s.avgDurationS || null)],
  ];
  $("cards").innerHTML = cards.map(([label, value, hint]) =>
    `<div class="card"><div class="card-label">${label}</div>` +
    `<div class="card-value">${value}</div>${hint ? `<div class="card-hint">${hint}</div>` : ""}</div>`
  ).join("");
}

function renderRepoSelect(d: Dashboard): void {
  const sel = $("repoSelect") as HTMLSelectElement;
  const repos = (d.repos ?? []).map((r) => r.repository);
  sel.innerHTML =
    `<option value="">全部仓库（${repos.length}）</option>` +
    repos.map((r) => `<option value="${r}"${r === state.repo ? " selected" : ""}>${r}</option>`).join("");
}

function renderCharts(d: Dashboard): void {
  for (const c of charts) c.destroy();
  charts.length = 0;
  const repos = d.repos ?? [];
  for (const r of repos) {
    if (!repoColors.has(r.repository)) repoColors.set(r.repository, REPO_PALETTE[repoColors.size % REPO_PALETTE.length]);
  }
  charts.push(createRepoBar($("repoChart") as HTMLCanvasElement, repos, repoMetric));
  charts.push(createVerdictDoughnut($("verdictChart") as HTMLCanvasElement, d.verdicts ?? []));
  charts.push(createTrendLine($("trendChart") as HTMLCanvasElement, d.trend ?? [], trendMetric, repoColors));
}

function renderRecent(d: Dashboard): void {
  const rows = d.recent ?? [];
  $("recentBody").innerHTML = rows.map((r) => {
    const vColor = VERDICT_COLORS[r.verdict] ?? "#94a3b8";
    const verdict = r.verdict === "" ? "—" : r.verdict;
    const sev = `${r.blocking} / ${r.warning}`;
    return `<tr>` +
      `<td class="mono">${fmtTs(r.ts)}</td>` +
      `<td class="mono" title="${r.repository}">${r.repository.split("/").pop()}</td>` +
      `<td class="mono"><a href="${prUrl(r)}" target="_blank" rel="noreferrer">#${r.pr}</a></td>` +
      `<td>${r.mode === "team" ? "团队" : "单评审"}</td>` +
      `<td><span class="verdict" style="color:${vColor}">${verdict}</span></td>` +
      `<td class="mono">${sev}</td>` +
      `<td class="mono num">${fmtTokens(r.input)}</td>` +
      `<td class="mono num">${fmtTokens(r.output)}</td>` +
      `<td class="mono num">${fmtTokens(r.cacheRead)}</td>` +
      `<td class="mono num">${fmtCost(r.costTotal)}</td>` +
      `<td class="mono num">${fmtDuration(r.durationS)}</td>` +
      `<td class="mono num">${r.personas}</td>` +
      `</tr>`;
  }).join("") || `<tr><td colspan="12" class="empty">该时间窗口内还没有评审记录</td></tr>`;
}

/** GitHub/Gitea PR link best-effort: platform column carries "github" or
 *  "gitea"; anything unknown stays a plain number. */
function prUrl(r: { platform: string; repository: string; pr: number }): string {
  if (r.platform === "github") return `https://github.com/${r.repository}/pull/${r.pr}`;
  if (r.platform === "gitea") return `https://gitea.com/${r.repository}/pulls/${r.pr}`;
  return "#";
}

function renderMeta(d: Dashboard): void {
  const repos = d.repos?.length ?? 0;
  $("dbMeta").textContent =
    d.summary.reviews === 0 ? "暂无数据 — 等 agent 推送第一条事件" : `${repos} 个仓库`;
}

async function load(): Promise<void> {
  $("recentBody").innerHTML = `<tr><td colspan="12" class="empty">加载中…</td></tr>`;
  try {
    data = await fetchDashboard(state);
  } catch (err) {
    $("recentBody").innerHTML =
      `<tr><td colspan="12" class="empty error">加载失败：${String(err)}</td></tr>`;
    return;
  }
  renderCards(data);
  renderRepoSelect(data);
  renderCharts(data);
  renderRecent(data);
  renderMeta(data);
}

function wireControls(): void {
  for (const btn of document.querySelectorAll<HTMLButtonElement>("#dayChips .chip")) {
    btn.addEventListener("click", () => {
      state.days = Number(btn.dataset.days);
      for (const b of document.querySelectorAll("#dayChips .chip")) b.classList.toggle("active", b === btn);
      void load();
    });
  }
  $("repoSelect").addEventListener("change", (e) => {
    state.repo = (e.target as HTMLSelectElement).value;
    void load();
  });
  for (const btn of document.querySelectorAll<HTMLButtonElement>("#repoMetric .chip")) {
    btn.addEventListener("click", () => {
      repoMetric = btn.dataset.metric as RepoMetric;
      for (const b of document.querySelectorAll("#repoMetric .chip")) b.classList.toggle("active", b === btn);
      if (data) renderCharts(data);
    });
  }
  for (const btn of document.querySelectorAll<HTMLButtonElement>("#trendMetric .chip")) {
    btn.addEventListener("click", () => {
      trendMetric = btn.dataset.metric as TrendMetric;
      for (const b of document.querySelectorAll("#trendMetric .chip")) b.classList.toggle("active", b === btn);
      if (data) renderCharts(data);
    });
  }
  // Keep the default window chip in sync with state.
  for (const b of document.querySelectorAll<HTMLButtonElement>("#dayChips .chip")) {
    b.classList.toggle("active", Number(b.dataset.days) === state.days);
  }
}

void load();
wireControls();
