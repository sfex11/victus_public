"""
BigVolver Daily Report Generator — Performance summary → Telegram.

Generates a formatted daily report and sends via Telegram.
Scheduled to run at 08:00 KST daily.
"""

import json
from datetime import datetime, timedelta, timezone
from pathlib import Path


KST = timezone(timedelta(hours=9))


class DailyReportGenerator:
    """Generates daily performance reports for Telegram."""

    def __init__(self, tracker, notifier=None):
        """
        Args:
            tracker: ExperimentTracker instance.
            notifier: TelegramNotifier (optional — for direct sending).
        """
        self.tracker = tracker
        self.notifier = notifier

    def generate_report(self, date: str = None) -> str:
        """
        Generate a daily performance report.

        Args:
            date: Date string (YYYY-MM-DD). Defaults to yesterday.

        Returns:
            Formatted report string.
        """
        if date is None:
            yesterday = datetime.now(KST) - timedelta(days=1)
            date = yesterday.strftime("%Y-%m-%d")

        date_display = datetime.strptime(date, "%Y-%m-%d").strftime("%Y-%m-%d (%a)")
        weekday = self._get_korean_weekday(date)

        # Fetch metrics from tracker
        ml_metrics = self.tracker.get_latest_metrics("lightgbm")
        ppo_metrics = self.tracker.get_latest_metrics("ppo")
        sac_metrics = self.tracker.get_latest_metrics("sac")

        # Build report
        lines = []
        lines.append(f"📊 <b>BigVolver 일일 성과 리포트</b>")
        lines.append(f"📅 {date_display} ({weekday})\n")

        # Model status
        lines.append("<b>🔧 모델 현황</b>")
        if ml_metrics:
            m = ml_metrics.get("metrics", {})
            lines.append(
                f"  ML (LightGBM): <code>{ml_metrics.get('version', '?')}</code>\n"
                f"    Sharpe: {m.get('sharpe_ratio', 0):.2f} | "
                f"Win Rate: {m.get('win_rate', 0)*100:.1f}%\n"
                f"    Samples: {ml_metrics.get('params', {}).get('samples', '?')}"
            )
        else:
            lines.append("  ML: 데이터 없음")

        if ppo_metrics:
            m = ppo_metrics.get("metrics", {})
            lines.append(
                f"  DRL (PPO): <code>{ppo_metrics.get('version', '?')}</code>\n"
                f"    Sharpe: {m.get('sharpe_ratio', 0):.2f} | "
                f"Win Rate: {m.get('win_rate', 0)*100:.1f}%"
            )

        if sac_metrics:
            m = sac_metrics.get("metrics", {})
            lines.append(
                f"  DRL (SAC): <code>{sac_metrics.get('version', '?')}</code>\n"
                f"    Sharpe: {m.get('sharpe_ratio', 0):.2f} | "
                f"Win Rate: {m.get('win_rate', 0)*100:.1f}%"
            )

        lines.append("")

        # Ensemble weights
        ml_sharpe = ml_metrics.get("metrics", {}).get("sharpe_ratio", 0) if ml_metrics else 0
        ppo_sharpe = ppo_metrics.get("metrics", {}).get("sharpe_ratio", 0) if ppo_metrics else 0
        sac_sharpe = sac_metrics.get("metrics", {}).get("sharpe_ratio", 0) if sac_metrics else 0
        drl_sharpe = max(ppo_sharpe, sac_sharpe)

        ml_w, drl_w = self._ensemble_weights(ml_sharpe, drl_sharpe)
        lines.append("<b>⚖️ 앙상블 비중</b>")
        lines.append(f"  ML: {ml_w*100:.0f}% | DRL: {drl_w*100:.0f}%")

        # Best model
        models = {
            "ML": ml_sharpe,
            "PPO": ppo_sharpe,
            "SAC": sac_sharpe,
        }
        best = max(models, key=models.get)
        lines.append(f"  최고 성능: <b>{best}</b> (Sharpe: {models[best]:.2f})")

        lines.append("")

        # Retrain history (last 24h)
        lines.append("<b>🔄 최근 재훈련</b>")
        retrain_history = self.tracker.get_retrain_history(limit=5)

        yesterday_ts = datetime.strptime(date, "%Y-%m-%d").replace(tzinfo=KST)

        recent_retrains = []
        for rt in retrain_history:
            try:
                rt_time = datetime.fromisoformat(rt["timestamp"])
                if rt_time.date() == yesterday_ts.date():
                    recent_retrains.append(rt)
            except (KeyError, ValueError):
                continue

        if recent_retrains:
            for rt in recent_retrains:
                status = "✅ 배포" if not rt.get("rolled_back") else "⏪ 롤백"
                lines.append(
                    f"  {status} {rt.get('symbol', '?')}: "
                    f"{rt.get('old_version', '?')} → {rt.get('new_version', '?')}\n"
                    f"    Sharpe: {rt.get('old_sharpe', 0):.2f} → {rt.get('new_sharpe', 0):.2f}"
                )
        else:
            lines.append("  없음")

        lines.append("")

        # Tracker status
        status = self.tracker.get_status()
        lines.append("<b>📡 시스템 상태</b>")
        lines.append(f"  Telemetry: {status['backend']}")
        if status.get("mlflow_connected"):
            lines.append("  MLflow: ✅ 연결됨")
        else:
            lines.append("  MLflow: ❌ 미연결 (로컬 저장 사용)")

        return "\n".join(lines)

    def send_report(self, date: str = None) -> bool:
        """Generate and send the report via Telegram.

        Returns:
            True if sent successfully.
        """
        if self.notifier is None:
            print("[DailyReport] No notifier configured. Printing report.")
            print(self.generate_report(date))
            return False

        report = self.generate_report(date)
        try:
            self.notifier.SendMessage(report)
            print(f"[DailyReport] Report sent to Telegram")
            return True
        except Exception as e:
            print(f"[DailyReport] Failed to send: {e}")
            return False

    def _ensemble_weights(self, ml_sharpe: float, drl_sharpe: float) -> tuple:
        """Calculate Sharpe² weighted ensemble weights."""
        ml_w = ml_sharpe ** 2
        drl_w = drl_sharpe ** 2
        total = ml_w + drl_w

        if total == 0:
            return (0.5, 0.5)

        return (ml_w / total, drl_w / total)

    @staticmethod
    def _get_korean_weekday(date_str: str) -> str:
        """Get Korean weekday name."""
        weekdays = ["월", "화", "수", "목", "금", "토", "일"]
        dt = datetime.strptime(date_str, "%Y-%m-%d")
        return weekdays[dt.weekday()]


if __name__ == "__main__":
    # Standalone usage
    import sys
    telemetry_path = str(Path(__file__).parent.parent / "telemetry")
    if telemetry_path not in sys.path:
        sys.path.insert(0, telemetry_path)

    from tracker import get_tracker

    tracker = get_tracker()
    generator = DailyReportGenerator(tracker)
    print(generator.generate_report())
