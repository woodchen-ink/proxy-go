package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type FeishuHandler struct {
	webhookURL string
	client     *http.Client
	cardPool   sync.Pool
}

func NewFeishuHandler(webhookURL string) *FeishuHandler {
	h := &FeishuHandler{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
	h.cardPool = sync.Pool{
		New: func() interface{} {
			return &FeishuCard{}
		},
	}
	return h
}

type FeishuCard struct {
	MsgType string `json:"msg_type"`
	Card    struct {
		Header struct {
			Title struct {
				Content string `json:"content"`
				Tag     string `json:"tag"`
			} `json:"title"`
		} `json:"header"`
		Elements []interface{} `json:"elements"`
	} `json:"card"`
}

func (h *FeishuHandler) HandleAlert(alert Alert) {
	card := h.cardPool.Get().(*FeishuCard)

	// 设置标题
	card.Card.Header.Title.Tag = "plain_text"
	card.Card.Header.Title.Content = fmt.Sprintf("[%s] 监控告警", alert.Level)

	// 添加告警内容
	content := map[string]interface{}{
		"tag": "div",
		"text": map[string]interface{}{
			"content": fmt.Sprintf("**告警时间**: %s\n**告警内容**: %s",
				alert.Time.Format("2006-01-02 15:04:05"),
				alert.Message),
			"tag": "lark_md",
		},
	}

	card.Card.Elements = []interface{}{content}

	// 发送请求
	payload, _ := json.Marshal(card)
	resp, err := h.client.Post(h.webhookURL, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		fmt.Printf("Failed to send Feishu alert: %v\n", err)
		return
	}
	defer resp.Body.Close()

	h.cardPool.Put(card)
}
