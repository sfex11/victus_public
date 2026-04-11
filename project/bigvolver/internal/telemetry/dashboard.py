"""
BigVolver Strategy Evolution Dashboard — Self-contained HTML.

Single HTML file with Chart.js for visualization.
Auto-refreshes by regenerating from telemetry data.
"""

import json
from datetime import datetime, timedelta, timezone
from pathlib import Path

KST = timezone(timedelta(hours=9))


class DashboardBuilder:
    """Builds a self-contained HTML dashboard."""

    def __init__(self, tracker, output_path: str = "./dashboard/index.html"):
        """
        Args:
            tracker: ExperimentTracker instance.
            output_path: Path to write the HTML dashboard.
        """
        self.tracker = tracker
        self.output_path = Path(output_path)
        self.output_path.parent.mkdir(parents=True, exist_ok=True)

    def build(self) -> str:
        """Build and write the dashboard HTML. Returns the file path."""
        # Collect data
        ml_metrics = self.tracker.get_latest_metrics("lightgbm")
        ppo_metrics = self.tracker.get_latest_metrics("ppo")
        sac_metrics = self.tracker.get_latest_metrics("sac")
        retrain_history = self.tracker.get_retrain_history(limit=20)
        tracker_status = self.tracker.get_status()

        # Build HTML
        html = self._render_html(
            ml_metrics=ml_metrics,
            ppo_metrics=ppo_metrics,
            sac_metrics=sac_metrics,
            retrain_history=retrain_history,
            tracker_status=tracker_status,
        )

        self.output_path.write_text(html, encoding="utf-8")
        return str(self.output_path)

    def _render_html(self, ml_metrics, ppo_metrics, sac_metrics, retrain_history, tracker_status) -> str:
        now = datetime.now(KST).strftime("%Y-%m-%d %H:%M KST")

        # Model data
        models_data = []
        for name, m in [("LightGBM (ML)", ml_metrics), ("PPO (DRL)", ppo_metrics), ("SAC (DRL)", sac_metrics)]:
            if m and m.get("metrics"):
                met = m["metrics"]
                models_data.append({
                    "name": name,
                    "version": m.get("version", "?"),
                    "sharpe": met.get("sharpe_ratio", 0),
                    "win_rate": met.get("win_rate", 0) * 100,
                    "mse": met.get("mse", 0),
                    "total_return": met.get("total_return", 0),
                    "max_drawdown": met.get("max_drawdown", 0),
                })

        # Retrain timeline data
        retrain_data = []
        for rt in retrain_history[-10:]:
            retrain_data.append({
                "time": rt.get("timestamp", "")[:16],
                "symbol": rt.get("symbol", "?"),
                "old_version": rt.get("old_version", "?"),
                "new_version": rt.get("new_version", "?"),
                "old_sharpe": rt.get("old_sharpe", 0),
                "new_sharpe": rt.get("new_sharpe", 0),
                "rolled_back": rt.get("rolled_back", False),
                "reason": rt.get("reason", ""),
            })

        models_json = json.dumps(models_data, ensure_ascii=False)
        retrain_json = json.dumps(retrain_data, ensure_ascii=False)

        backend_badge = "🟢 MLflow" if tracker_status.get("mlflow_connected") else "🟡 Local"

        # Phase status
        phases = [
            ("A", "ML 파이프라인", "✅ 완료"),
            ("B", "Self-Adaptive Retraining", "✅ 완료"),
            ("C", "Weight-Centric Pipeline", "✅ 완료"),
            ("D", "DRL 통합", "✅ 완료"),
            ("E", "실험 추적 & 모니터링", "✅ 완료"),
        ]

        return f"""<!DOCTYPE html>
<html lang="ko">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>BigVolver V2 — 전략 진화 대시보드</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
<style>
  * {{ margin: 0; padding: 0; box-sizing: border-box; }}
  body {{
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    background: #0f1117;
    color: #e1e4e8;
    padding: 20px;
  }}
  .header {{
    text-align: center;
    padding: 30px;
    background: linear-gradient(135deg, #1a1a2e 0%, #16213e 100%);
    border-radius: 16px;
    margin-bottom: 20px;
  }}
  .header h1 {{
    font-size: 28px;
    background: linear-gradient(90deg, #60a5fa, #a78bfa);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    margin-bottom: 8px;
  }}
  .header .meta {{
    color: #8b949e;
    font-size: 14px;
  }}
  .grid {{
    display: grid;
    grid-template-columns: 280px 1fr;
    gap: 20px;
  }}
  @media (max-width: 900px) {{
    .grid {{ grid-template-columns: 1fr; }}
  }}
  .sidebar {{
    display: flex;
    flex-direction: column;
    gap: 16px;
  }}
  .card {{
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 12px;
    padding: 20px;
  }}
  .card h2 {{
    font-size: 16px;
    color: #58a6ff;
    margin-bottom: 16px;
    display: flex;
    align-items: center;
    gap: 8px;
  }}
  .phase-item {{
    display: flex;
    align-items: center;
    padding: 8px 0;
    border-bottom: 1px solid #21262d;
  }}
  .phase-item:last-child {{ border-bottom: none; }}
  .phase-badge {{
    width: 32px;
    height: 32px;
    background: #21262d;
    border-radius: 8px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-weight: bold;
    font-size: 14px;
    color: #58a6ff;
    margin-right: 12px;
    flex-shrink: 0;
  }}
  .phase-name {{ font-size: 14px; }}
  .phase-status {{ font-size: 12px; color: #8b949e; }}
  .model-card {{
    background: #1c2128;
    border: 1px solid #30363d;
    border-radius: 8px;
    padding: 12px;
    margin-bottom: 8px;
  }}
  .model-card .name {{
    font-weight: 600;
    font-size: 14px;
    margin-bottom: 4px;
  }}
  .model-card .version {{
    font-size: 11px;
    color: #8b949e;
    font-family: monospace;
    margin-bottom: 8px;
  }}
  .metric {{
    display: flex;
    justify-content: space-between;
    font-size: 13px;
    padding: 2px 0;
  }}
  .metric .label {{ color: #8b949e; }}
  .metric .value {{ font-weight: 600; }}
  .metric .value.positive {{ color: #3fb950; }}
  .metric .value.negative {{ color: #f85149; }}
  .chart-container {{
    position: relative;
    height: 250px;
    margin-bottom: 16px;
  }}
  .retrain-item {{
    padding: 8px 0;
    border-bottom: 1px solid #21262d;
    font-size: 13px;
  }}
  .retrain-item:last-child {{ border-bottom: none; }}
  .retrain-time {{ color: #8b949e; font-size: 11px; }}
  .badge {{
    display: inline-block;
    padding: 2px 8px;
    border-radius: 12px;
    font-size: 11px;
    font-weight: 600;
  }}
  .badge-deploy {{ background: #0d4429; color: #3fb950; }}
  .badge-rollback {{ background: #4c1d2c; color: #f85149; }}
  .status-dot {{
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    margin-right: 6px;
  }}
  .status-green {{ background: #3fb950; }}
  .status-yellow {{ background: #d29922; }}
</style>
</head>
<body>

<div class="header">
  <h1>⬡ BigVolver V2</h1>
  <p class="meta">전략 진화 대시보드 | Last updated: {now}</p>
</div>

<div class="grid">
  <!-- Sidebar -->
  <div class="sidebar">
    <div class="card">
      <h2>📋 Phase 현황</h2>
      {"".join(f'<div class="phase-item"><div class="phase-badge">{p[0]}</div><div><div class="phase-name">{p[1]}</div><div class="phase-status">{p[2]}</div></div></div>' for p in phases)}
    </div>

    <div class="card">
      <h2>📡 시스템</h2>
      <div class="metric">
        <span class="label">Telemetry</span>
        <span class="value">{backend_badge}</span>
      </div>
      <div class="metric">
        <span class="label">Experiment</span>
        <span class="value">{tracker_status.get("experiment", "bigvolver-v2")}</span>
      </div>
      <div class="metric">
        <span class="label">Predictions</span>
        <span class="value">{tracker_status.get("prediction_counter", 0)}</span>
      </div>
    </div>
  </div>

  <!-- Main Content -->
  <div>
    <div class="card" style="margin-bottom: 20px;">
      <h2>🤖 모델 비교</h2>
      <div id="model-cards"></div>
    </div>

    <div class="card" style="margin-bottom: 20px;">
      <h2>📊 Sharpe Ratio 비교</h2>
      <div class="chart-container">
        <canvas id="sharpeChart"></canvas>
      </div>
    </div>

    <div class="card" style="margin-bottom: 20px;">
      <h2>🎯 Win Rate 비교 (%)</h2>
      <div class="chart-container">
        <canvas id="winRateChart"></canvas>
      </div>
    </div>

    <div class="card">
      <h2>🔄 재훈련 이력</h2>
      <div id="retrain-timeline"></div>
    </div>
  </div>
</div>

<script>
const models = {models_json};
const retrains = {retrain_json};

// Render model cards
const cardsContainer = document.getElementById('model-cards');
models.forEach(m => {{
  const sharpeClass = m.sharpe > 1 ? 'positive' : m.sharpe < 0 ? 'negative' : '';
  cardsContainer.innerHTML += `
    <div class="model-card">
      <div class="name">${{m.name}}</div>
      <div class="version">${{m.version}}</div>
      <div class="metric"><span class="label">Sharpe</span><span class="value ${{sharpeClass}}">${{m.sharpe.toFixed(2)}}</span></div>
      <div class="metric"><span class="label">Win Rate</span><span class="value">${{m.win_rate.toFixed(1)}}%</span></div>
      <div class="metric"><span class="label">Max DD</span><span class="value">${{m.max_drawdown.toFixed(2)}}%</span></div>
    </div>
  `;
}});

// Sharpe chart
new Chart(document.getElementById('sharpeChart'), {{
  type: 'bar',
  data: {{
    labels: models.map(m => m.name),
    datasets: [{{
      label: 'Sharpe Ratio',
      data: models.map(m => m.sharpe),
      backgroundColor: ['#60a5fa', '#f472b6', '#34d399'],
      borderRadius: 6,
    }}]
  }},
  options: {{
    responsive: true,
    maintainAspectRatio: false,
    plugins: {{ legend: {{ display: false }} }},
    scales: {{
      y: {{
        grid: {{ color: '#21262d' }},
        ticks: {{ color: '#8b949e' }}
      }},
      x: {{
        grid: {{ display: false }},
        ticks: {{ color: '#8b949e' }}
      }}
    }}
  }}
}});

// Win Rate chart
new Chart(document.getElementById('winRateChart'), {{
  type: 'bar',
  data: {{
    labels: models.map(m => m.name),
    datasets: [{{
      label: 'Win Rate (%)',
      data: models.map(m => m.win_rate),
      backgroundColor: ['#60a5fa', '#f472b6', '#34d399'],
      borderRadius: 6,
    }}]
  }},
  options: {{
    responsive: true,
    maintainAspectRatio: false,
    plugins: {{ legend: {{ display: false }} }},
    scales: {{
      y: {{
        grid: {{ color: '#21262d' }},
        ticks: {{ color: '#8b949e' }}
      }},
      x: {{
        grid: {{ display: false }},
        ticks: {{ color: '#8b949e' }}
      }}
    }}
  }}
}});

// Retrain timeline
const timelineContainer = document.getElementById('retrain-timeline');
if (retrains.length === 0) {{
  timelineContainer.innerHTML = '<p style="color:#8b949e">재훈련 이력 없음</p>';
}} else {{
  retrains.forEach(rt => {{
    const badge = rt.rolled_back
      ? '<span class="badge badge-rollback">⏪ 롤백</span>'
      : '<span class="badge badge-deploy">✅ 배포</span>';
    const change = rt.new_sharpe - rt.old_sharpe;
    const changeColor = change >= 0 ? '#3fb950' : '#f85149';
    timelineContainer.innerHTML += `
      <div class="retrain-item">
        ${{badge}} <strong>${{rt.symbol}}</strong>
        <span class="retrain-time"> ${{rt.time}}</span><br>
        Sharpe: ${{rt.old_sharpe.toFixed(2)}} → ${{rt.new_sharpe.toFixed(2)}}
        <span style="color:${{changeColor}}">(${{change >= 0 ? '+' : ''}}${{change.toFixed(2)}})</span>
      </div>
    `;
  }});
}}
</script>

</body>
</html>"""


if __name__ == "__main__":
    import sys
    telemetry_path = str(Path(__file__).parent.parent / "telemetry")
    if telemetry_path not in sys.path:
        sys.path.insert(0, telemetry_path)

    from tracker import get_tracker

    tracker = get_tracker()
    builder = DashboardBuilder(tracker)
    path = builder.build()
    print(f"[Dashboard] Generated: {path}")
