"""
NFI vs BigVolver 듀얼 비교 대시보드
기존 dashboard.py 스타일을 따르며, 두 시스템의 성과를 비교하는 HTML 생성
"""

import json
from datetime import datetime, timedelta, timezone
from pathlib import Path

KST = timezone(timedelta(hours=9))


class ComparatorDashboard:
    """NFI vs BigVolver 비교 대시보드 빌더"""

    def __init__(self, dual_engine=None, output_path: str = "./dashboard/comparator.html"):
        self.engine = dual_engine
        self.output_path = Path(output_path)
        self.output_path.parent.mkdir(parents=True, exist_ok=True)

    def build_from_api(self, api_url: str = "http://localhost:8080") -> str:
        """Go DualEngine API에서 데이터를 가져와 대시보드 생성"""
        try:
            import urllib.request
            req = urllib.request.urlopen(f"{api_url}/api/v1/status", timeout=5)
            status = json.loads(req.read().decode())
            req = urllib.request.urlopen(f"{api_url}/api/v1/compare", timeout=5)
            compare = json.loads(req.read().decode())
            req = urllib.request.urlopen(f"{api_url}/api/v1/signals", timeout=5)
            signals = json.loads(req.read().decode())
        except Exception as e:
            status = {"running": False, "nfi": {}, "bigvolver": {}, "comparison": None, "signal_agreement": 0}
            compare = None
            signals = []

        html = self._render(status, compare, signals)
        self.output_path.write_text(html, encoding="utf-8")
        return str(self.output_path)

    def build_from_data(self, status: dict, compare: dict, signals: list) -> str:
        """직접 데이터를 받아 대시보드 생성"""
        html = self._render(status, compare, signals)
        self.output_path.write_text(html, encoding="utf-8")
        return str(self.output_path)

    def _render(self, status: dict, compare: dict, signals: list) -> str:
        now = datetime.now(KST).strftime("%Y-%m-%d %H:%M KST")
        running = status.get("running", False)
        initial = status.get("initial_capital", 10000)

        nfi = status.get("nfi", {})
        bv = status.get("bigvolver", {})
        cmp = compare or {}
        agreement = status.get("signal_agreement", 0)

        nfi_pnl = nfi.get("total_pnl", 0)
        bv_pnl = bv.get("total_pnl", 0)
        nfi_equity = nfi.get("equity", initial)
        bv_equity = bv.get("equity", initial)

        status_dot = "🟢" if running else "🔴"
        status_text = "실행 중" if running else "중지됨"

        # 시그널 최근 20개
        recent_signals = (signals[-20:] if isinstance(signals, list) else [])[:20]

        signals_html = ""
        for s in recent_signals:
            ts = s.get("timestamp", "")[:19].replace("T", " ")
            src = s.get("source", "?")
            pair = s.get("pair", "?")
            direction = s.get("direction", "?")
            color = "#3fb950" if direction == "LONG" else "#f85149" if direction == "CLOSE" else "#d29922"
            mode = s.get("mode", "")
            signals_html += f"""
            <div class="signal-item">
                <span class="signal-source">{"🔵 NFI" if src == "NFI" else "🟣 BigV"}</span>
                <span class="signal-pair">{pair}</span>
                <span class="signal-dir" style="color:{color}">{direction}</span>
                <span class="signal-mode">{mode}</span>
                <span class="signal-time">{ts}</span>
            </div>"""

        if not signals_html:
            signals_html = '<p style="color:#8b949e">시그널 없음</p>'

        return f"""<!DOCTYPE html>
<html lang="ko">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>NFI vs BigVolver — 듀얼 비교 대시보드</title>
<meta http-equiv="refresh" content="300">
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
    background: linear-gradient(90deg, #60a5fa, #a78bfa, #f472b6);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    margin-bottom: 8px;
  }}
  .header .meta {{ color: #8b949e; font-size: 14px; }}
  .grid {{
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 20px;
  }}
  @media (max-width: 900px) {{ .grid {{ grid-template-columns: 1fr; }} }}
  .full-width {{ grid-column: 1 / -1; }}
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
  .system-card {{
    border-left: 4px solid;
    padding-left: 16px;
  }}
  .system-nfi {{ border-color: #60a5fa; }}
  .system-bv {{ border-color: #a78bfa; }}
  .winner {{ border-left-color: #3fb950 !important; box-shadow: 0 0 20px rgba(63,185,80,0.1); }}
  .metric {{
    display: flex;
    justify-content: space-between;
    font-size: 13px;
    padding: 4px 0;
    border-bottom: 1px solid #21262d;
  }}
  .metric:last-child {{ border-bottom: none; }}
  .metric .label {{ color: #8b949e; }}
  .metric .value {{ font-weight: 600; font-family: monospace; }}
  .metric .value.positive {{ color: #3fb950; }}
  .metric .value.negative {{ color: #f85149; }}
  .chart-container {{
    position: relative;
    height: 250px;
  }}
  .signal-item {{
    display: flex;
    gap: 8px;
    padding: 6px 0;
    border-bottom: 1px solid #21262d;
    font-size: 12px;
    align-items: center;
  }}
  .signal-source {{ min-width: 70px; font-weight: 600; }}
  .signal-pair {{ min-width: 80px; }}
  .signal-dir {{ min-width: 50px; font-weight: 600; }}
  .signal-mode {{ color: #8b949e; min-width: 80px; font-size: 11px; }}
  .signal-time {{ color: #8b949e; font-size: 11px; margin-left: auto; }}
  .badge {{
    display: inline-block;
    padding: 2px 10px;
    border-radius: 12px;
    font-size: 12px;
    font-weight: 600;
  }}
  .badge-nfi {{ background: #1c3a5e; color: #60a5fa; }}
  .badge-bv {{ background: #2d1f5e; color: #a78bfa; }}
  .vs-badge {{
    background: #21262d;
    padding: 4px 12px;
    border-radius: 16px;
    font-size: 14px;
    font-weight: 700;
  }}
  .summary-row {{
    display: flex;
    justify-content: center;
    gap: 20px;
    margin-bottom: 20px;
    align-items: center;
    flex-wrap: wrap;
  }}
  .agreement-bar {{
    width: 100%;
    height: 8px;
    background: #21262d;
    border-radius: 4px;
    overflow: hidden;
    margin-top: 8px;
  }}
  .agreement-fill {{
    height: 100%;
    border-radius: 4px;
    transition: width 0.3s;
  }}
</style>
</head>
<body>

<div class="header">
  <h1>⚔️ NFI vs BigVolver</h1>
  <p class="meta">듀얼 가상매매 비교 대시보드 | {status_dot} {status_text} | Last updated: {now}</p>
  <p class="meta">초기 자본: {initial:,.0f} USDT (각 시스템) | 수수료: 0.1% | 슬리피지: 1%</p>
</div>

<!-- 요약 카드 -->
<div class="card full-width" style="margin-bottom: 20px;">
  <div class="summary-row">
    <span class="badge badge-nfi">🔵 NFI</span>
    <span style="font-size:24px;font-weight:700;{'color:#3fb950' if nfi_pnl >= 0 else 'color:#f85149'}">
      {nfi_pnl:+,.2f} USDT ({nfi_pnl/initial*100:+.2f}%)
    </span>
    <span class="vs-badge">VS</span>
    <span style="font-size:24px;font-weight:700;{'color:#3fb950' if bv_pnl >= 0 else 'color:#f85149'}">
      {bv_pnl:+,.2f} USDT ({bv_pnl/initial*100:+.2f}%)
    </span>
    <span class="badge badge-bv">🟣 BigV</span>
  </div>
  <div style="text-align:center; color:#8b949e; font-size:13px;">
    시그널 일치도:
    <strong style="color:#e1e4e8">{agreement*100:.0f}%</strong>
    <div class="agreement-bar" style="max-width:300px;margin:4px auto 0;">
      <div class="agreement-fill" style="width:{agreement*100}%;background:{'#3fb950' if agreement > 0.5 else '#d29922' if agreement > 0.25 else '#f85149'}"></div>
    </div>
  </div>
</div>

<div class="grid">
  <!-- NFI 상세 -->
  <div class="card system-card system-nfi {'winner' if nfi_pnl > bv_pnl else ''}">
    <h2>🔵 NostalgiaForInfinity</h2>
    <div class="metric"><span class="label">자산</span><span class="value">{nfi_equity:,.2f} USDT</span></div>
    <div class="metric"><span class="label">총 수익</span><span class="value {'positive' if nfi_pnl >= 0 else 'negative'}">{nfi_pnl:+,.2f} USDT</span></div>
    <div class="metric"><span class="label">Sharpe Ratio</span><span class="value">{nfi.get('sharpe', 0):.2f}</span></div>
    <div class="metric"><span class="label">승률</span><span class="value">{nfi.get('win_rate', 0):.1f}%</span></div>
    <div class="metric"><span class="label">Max Drawdown</span><span class="value negative">{nfi.get('max_dd', 0):.2f}%</span></div>
    <div class="metric"><span class="label">총 거래수</span><span class="value">{nfi.get('total_trades', 0)}</span></div>
    <div class="metric"><span class="label">오픈 포지션</span><span class="value">{nfi.get('open_positions', 0)}</span></div>
  </div>

  <!-- BigV 상세 -->
  <div class="card system-card system-bv {'winner' if bv_pnl > nfi_pnl else ''}">
    <h2>🟣 BigVolver ML</h2>
    <div class="metric"><span class="label">자산</span><span class="value">{bv_equity:,.2f} USDT</span></div>
    <div class="metric"><span class="label">총 수익</span><span class="value {'positive' if bv_pnl >= 0 else 'negative'}">{bv_pnl:+,.2f} USDT</span></div>
    <div class="metric"><span class="label">Sharpe Ratio</span><span class="value">{bv.get('sharpe', 0):.2f}</span></div>
    <div class="metric"><span class="label">승률</span><span class="value">{bv.get('win_rate', 0):.1f}%</span></div>
    <div class="metric"><span class="label">Max Drawdown</span><span class="value negative">{bv.get('max_dd', 0):.2f}%</span></div>
    <div class="metric"><span class="label">총 거래수</span><span class="value">{bv.get('total_trades', 0)}</span></div>
    <div class="metric"><span class="label">오픈 포지션</span><span class="value">{bv.get('open_positions', 0)}</span></div>
  </div>

  <!-- PnL 비교 차트 -->
  <div class="card">
    <h2>📊 수익률 비교</h2>
    <div class="chart-container">
      <canvas id="pnlChart"></canvas>
    </div>
  </div>

  <!-- 지표 비교 차트 -->
  <div class="card">
    <h2>🎯 핵심 지표 비교</h2>
    <div class="chart-container">
      <canvas id="metricsChart"></canvas>
    </div>
  </div>

  <!-- Alpha 분석 -->
  <div class="card">
    <h2>🧬 Alpha 분석 (시그널 출처별)</h2>
    <div class="metric"><span class="label">NFI만 진입</span><span class="value {'positive' if (cmp.get('nfi_only_alpha', 0) or 0) >= 0 else 'negative'}">{(cmp.get('nfi_only_alpha', 0) or 0):+.3f}%</span></div>
    <div class="metric"><span class="label">BigV만 진입</span><span class="value {'positive' if (cmp.get('bv_only_alpha', 0) or 0) >= 0 else 'negative'}">{(cmp.get('bv_only_alpha', 0) or 0):+.3f}%</span></div>
    <div class="metric"><span class="label">둘 다 진입</span><span class="value {'positive' if (cmp.get('both_win_alpha', 0) or 0) >= 0 else 'negative'}">{(cmp.get('both_win_alpha', 0) or 0):+.3f}%</span></div>
    <div class="metric"><span class="label">Delta (BigV - NFI)</span><span class="value {'positive' if (cmp.get('delta_pnl', 0) or 0) >= 0 else 'negative'}">{(cmp.get('delta_pnl', 0) or 0):+.2f} USDT</span></div>
  </div>

  <!-- 시그널 로그 -->
  <div class="card">
    <h2>📡 최근 시그널</h2>
    {signals_html}
  </div>
</div>

<script>
// PnL 비교 차트
new Chart(document.getElementById('pnlChart'), {{
  type: 'bar',
  data: {{
    labels: ['총 수익 (USDT)', '수익률 (%)'],
    datasets: [
      {{
        label: 'NFI',
        data: [{nfi_pnl:.2f}, {(nfi_pnl/initial*100):.2f}],
        backgroundColor: '#60a5fa',
        borderRadius: 6,
      }},
      {{
        label: 'BigVolver',
        data: [{bv_pnl:.2f}, {(bv_pnl/initial*100):.2f}],
        backgroundColor: '#a78bfa',
        borderRadius: 6,
      }}
    ]
  }},
  options: {{
    responsive: true,
    maintainAspectRatio: false,
    plugins: {{ legend: {{ labels: {{ color: '#8b949e' }} }} }},
    scales: {{
      y: {{ grid: {{ color: '#21262d' }}, ticks: {{ color: '#8b949e' }} }},
      x: {{ grid: {{ display: false }}, ticks: {{ color: '#8b949e' }} }}
    }}
  }}
}});

// 핵심 지표 비교 차트
new Chart(document.getElementById('metricsChart'), {{
  type: 'bar',
  data: {{
    labels: ['Sharpe Ratio', '승률 (%)', 'Max DD (%)'],
    datasets: [
      {{
        label: 'NFI',
        data: [{nfi.get('sharpe', 0):.2f}, {nfi.get('win_rate', 0):.1f}, -Math.abs(nfi.get('max_dd', 0) or 0):.2f}],
        backgroundColor: '#60a5fa',
        borderRadius: 6,
      }},
      {{
        label: 'BigVolver',
        data: [{bv.get('sharpe', 0):.2f}, {bv.get('win_rate', 0):.1f}, -Math.abs(bv.get('max_dd', 0) or 0):.2f}],
        backgroundColor: '#a78bfa',
        borderRadius: 6,
      }}
    ]
  }},
  options: {{
    responsive: true,
    maintainAspectRatio: false,
    plugins: {{ legend: {{ labels: {{ color: '#8b949e' }} }} }},
    scales: {{
      y: {{ grid: {{ color: '#21262d' }}, ticks: {{ color: '#8b949e' }} }},
      x: {{ grid: {{ display: false }}, ticks: {{ color: '#8b949e' }} }}
    }}
  }}
}});
</script>

</body>
</html>"""


if __name__ == "__main__":
    builder = ComparatorDashboard()
    # API에서 데이터 가져와서 생성 (DualEngine이 실행 중이어야 함)
    try:
        path = builder.build_from_api()
        print(f"[ComparatorDashboard] Generated: {{path}}")
    except Exception as e:
        print(f"[ComparatorDashboard] API 연결 실패: {{e}}")
        print("빈 대시보드를 생성합니다...")
        path = builder.build_from_data(
            {{"running": False, "initial_capital": 10000, "nfi": {{}}, "bigvolver": {{}}, "signal_agreement": 0}},
            {{}}, []
        )
        print(f"[ComparatorDashboard] Generated: {{path}}")
