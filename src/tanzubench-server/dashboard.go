package main

import (
	"fmt"
	"net/http"
)

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>TanzuBench Dashboard</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #0a0a0a; color: #ededed; }
  .container { max-width: 900px; margin: 0 auto; padding: 2rem; }
  h1 { font-size: 1.5rem; margin-bottom: 1.5rem; }
  .card { background: #1a1a1a; border: 1px solid #333; border-radius: 8px; padding: 1.5rem; margin-bottom: 1rem; }
  .card h2 { font-size: 1.1rem; margin-bottom: 0.75rem; color: #a0a0a0; }
  .status { font-size: 1.25rem; font-weight: 600; }
  .status.idle { color: #4ade80; }
  .status.running { color: #facc15; }
  .btn { display: inline-block; padding: 0.75rem 1.5rem; border-radius: 6px; border: none; font-size: 0.95rem; font-weight: 600; cursor: pointer; transition: all 0.15s; }
  .btn-primary { background: #3b82f6; color: white; }
  .btn-primary:hover { background: #2563eb; }
  .btn-primary:disabled { background: #555; cursor: not-allowed; }
  .btn-export { background: #10b981; color: white; margin-left: 0.5rem; }
  .btn-export:hover { background: #059669; }
  .log-box { background: #111; border: 1px solid #333; border-radius: 6px; padding: 1rem; margin-top: 1rem; font-family: monospace; font-size: 0.8rem; max-height: 400px; overflow-y: auto; white-space: pre-wrap; color: #a0a0a0; }
  .nav { display: flex; gap: 1rem; margin-bottom: 2rem; }
  .nav a { color: #3b82f6; text-decoration: none; }
  .nav a:hover { text-decoration: underline; }
  .results-table { width: 100%; border-collapse: collapse; margin-top: 0.75rem; }
  .results-table th, .results-table td { text-align: left; padding: 0.5rem; border-bottom: 1px solid #333; font-size: 0.85rem; }
  .results-table th { color: #888; font-weight: 600; text-transform: uppercase; font-size: 0.7rem; }
  .score { font-weight: 700; font-variant-numeric: tabular-nums; }
</style>
</head>
<body>
<div class="container">
  <div class="nav">
    <a href="/">← Leaderboard</a>
    <a href="/dashboard">Dashboard</a>
  </div>
  <h1>TanzuBench Dashboard</h1>

  <div class="card">
    <h2>Benchmark Runner</h2>
    <div id="status-text" class="status idle">Checking...</div>
    <div style="margin-top: 1rem;">
      <button id="run-btn" class="btn btn-primary" onclick="startRun()">Run Benchmark</button>
      <a href="/api/export" class="btn btn-export" download>Export Results</a>
    </div>
    <div id="log-container" style="display:none;">
      <div id="log" class="log-box"></div>
    </div>
  </div>

  <div class="card">
    <h2>Results</h2>
    <div id="results-container">Loading...</div>
  </div>
</div>

<script>
async function pollStatus() {
  try {
    const r = await fetch('/api/status');
    const d = await r.json();
    const el = document.getElementById('status-text');
    const btn = document.getElementById('run-btn');
    const logC = document.getElementById('log-container');
    const logEl = document.getElementById('log');

    if (d.running) {
      el.className = 'status running';
      el.textContent = 'Running: ' + (d.model || 'unknown') + ' (started ' + new Date(d.started_at).toLocaleTimeString() + ')';
      btn.disabled = true;
      btn.textContent = 'Running...';
      logC.style.display = 'block';
      if (d.log) { logEl.textContent = d.log; logEl.scrollTop = logEl.scrollHeight; }
    } else {
      el.className = 'status idle';
      el.textContent = 'Idle';
      btn.disabled = false;
      btn.textContent = 'Run Benchmark';
      if (d.last_run) {
        el.textContent = 'Last run: ' + d.last_run.model + ' (' + d.last_run.duration + ')' + (d.last_run.error ? ' ERROR: ' + d.last_run.error : '');
      }
      if (d.log) { logC.style.display = 'block'; logEl.textContent = d.log; }
    }
  } catch (e) { console.error(e); }
}

async function startRun() {
  if (!confirm('Start a benchmark run with the configured model?')) return;
  try {
    const r = await fetch('/api/run', { method: 'POST' });
    const d = await r.json();
    if (d.error) alert(d.error);
  } catch (e) { alert('Failed: ' + e); }
  pollStatus();
}

async function loadResults() {
  try {
    const r = await fetch('/api/results');
    const results = await r.json();
    const el = document.getElementById('results-container');
    if (!results || results.length === 0) {
      el.innerHTML = '<p style="color:#666">No results yet. Run a benchmark to populate.</p>';
      return;
    }
    // Sort by composite descending
    results.sort((a, b) => (b.summary?.composite_score || 0) - (a.summary?.composite_score || 0));
    let html = '<table class="results-table"><thead><tr><th>#</th><th>Model</th><th>Foundation</th><th>HW</th><th>Composite</th><th>Categories</th><th>Date</th></tr></thead><tbody>';
    results.forEach((d, i) => {
      const cats = d.summary?.category_scores?.filter(c => c.status === 'scored').length || 0;
      const total = d.summary?.category_scores?.length || 0;
      html += '<tr>' +
        '<td>' + (i+1) + '</td>' +
        '<td><strong>' + (d.target?.display_name || d.target?.name || '?') + '</strong></td>' +
        '<td>' + (d.meta?.foundation || '?') + '</td>' +
        '<td>' + (d.hardware?.gpu_count > 0 ? 'GPU' : 'CPU') + '</td>' +
        '<td class="score">' + (d.summary?.composite_score?.toFixed(3) || '—') + '</td>' +
        '<td>' + cats + '/' + total + '</td>' +
        '<td>' + new Date(d.meta?.timestamp).toLocaleDateString() + '</td>' +
        '</tr>';
    });
    html += '</tbody></table>';
    el.innerHTML = html;
  } catch (e) { document.getElementById('results-container').textContent = 'Error loading results'; }
}

// Poll every 3s when running, 10s when idle
setInterval(pollStatus, 5000);
pollStatus();
loadResults();
setInterval(loadResults, 30000);
</script>
</body>
</html>`

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, dashboardHTML)
}
