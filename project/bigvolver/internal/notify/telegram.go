package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// TelegramNotifier sends messages to a Telegram chat via bot API
type TelegramNotifier struct {
	botToken  string
	chatID    string
	client    *http.Client
	parseMode string // "HTML" or "Markdown"
}

// NewTelegramNotifier creates a new Telegram notifier
func NewTelegramNotifier(botToken, chatID string) *TelegramNotifier {
	return &TelegramNotifier{
		botToken:  botToken,
		chatID:    chatID,
		client:    &http.Client{Timeout: 10 * time.Second},
		parseMode: "HTML",
	}
}

// SendMessage sends a text message to the configured Telegram chat
func (n *TelegramNotifier) SendMessage(text string) error {
	payload := map[string]string{
		"chat_id":    n.chatID,
		"text":       text,
		"parse_mode": n.parseMode,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal telegram payload: %w", err)
	}

	resp, err := n.client.Post(
		fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.botToken),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("telegram API error %d: %v", resp.StatusCode, errResp)
	}

	return nil
}

// NotifyRetrainResult sends a formatted retrain result notification
func (n *TelegramNotifier) NotifyRetrainResult(symbol, modelVersion string, sharpe, winRate float64, samplesUsed int, err error) {
	if err != nil {
		n.SendMessage(fmt.Sprintf(
			"❌ <b>재훈련 실패</b>\n\n<b>종목:</b> %s\n<b>오류:</b> %s",
			symbol, err.Error(),
		))
		return
	}

	// Sharpe emoji based on quality
	sharpeEmoji := "🔴"
	if sharpe > 1.0 {
		sharpeEmoji = "🟢"
	} else if sharpe > 0.5 {
		sharpeEmoji = "🟡"
	}

	n.SendMessage(fmt.Sprintf(
		"🔄 <b>모델 재훈련 완료</b>\n\n"+
			"<b>종목:</b> %s\n"+
			"<b>버전:</b> %s\n"+
			"<b>샘플:</b> %d\n\n"+
			"%s <b>Sharpe:</b> %.4f\n"+
			"📊 <b>Win Rate:</b> %.1f%%",
		symbol, modelVersion, samplesUsed,
		sharpeEmoji, sharpe, winRate*100,
	))
}

// NotifyPerformanceAlert sends a performance degradation alert
func (n *TelegramNotifier) NotifyPerformanceAlert(symbol string, currentSharpe, previousSharpe float64, dropPct float64) {
	n.SendMessage(fmt.Sprintf(
		"⚠️ <b>성능 저하 경고</b>\n\n"+
			"<b>종목:</b> %s\n"+
			"<b>Sharpe:</b> %.4f → %.4f\n"+
			"<b>하락폭:</b> %.1f%%\n\n"+
			"자동 롤백이 고려됩니다.",
		symbol, previousSharpe, currentSharpe, dropPct,
	))
}

// NotifyModelRollback sends a rollback notification
func (n *TelegramNotifier) NotifyModelRollback(symbol, fromVersion, toVersion string, reason string) {
	n.SendMessage(fmt.Sprintf(
		"⏪ <b>모델 롤백</b>\n\n"+
			"<b>종목:</b> %s\n"+
			"<b>%s → %s</b>\n"+
			"<b>사유:</b> %s",
		symbol, fromVersion, toVersion, reason,
	))
}

// NotifyDailyReport sends a daily performance summary
func (n *TelegramNotifier) NotifyDailyReport(symbol string, sharpe, mdd, winRate, totalReturn float64, trades int) {
	emoji := "📈"
	if totalReturn < 0 {
		emoji = "📉"
	}

	n.SendMessage(fmt.Sprintf(
		"%s <b>일일 성과 리포트</b>\n\n"+
			"<b>종목:</b> %s\n"+
			"📊 <b>Sharpe:</b> %.4f\n"+
			"📉 <b>Max DD:</b> %.2f%%\n"+
			"🎯 <b>Win Rate:</b> %.1f%%\n"+
			"💰 <b>수익률:</b> %.2f%%\n"+
			"🔁 <b>거래수:</b> %d",
		emoji, symbol, sharpe, mdd, winRate*100, totalReturn, trades,
	))
}
