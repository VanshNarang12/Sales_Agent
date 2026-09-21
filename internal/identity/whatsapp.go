package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// OTPSender delivers a one-time code to a phone. Meta Cloud API is the production
// impl; swapping providers (Twilio/MSG91) means implementing this one interface.
type OTPSender interface {
	SendCode(ctx context.Context, phoneE164, code string) error
}

// MetaSender sends the code through the WhatsApp Cloud API using an approved
// authentication template (fixed content; the code fills body + copy-code button).
type MetaSender struct {
	http     *http.Client
	base     string
	phoneID  string
	token    string
	template string
	lang     string
}

func NewMetaSender(base, phoneID, token, template, lang string) *MetaSender {
	return &MetaSender{
		http:     &http.Client{Timeout: 10 * time.Second},
		base:     base,
		phoneID:  phoneID,
		token:    token,
		template: template,
		lang:     lang,
	}
}

func (s *MetaSender) SendCode(ctx context.Context, phoneE164, code string) error {
	param := []map[string]string{{"type": "text", "text": code}}
	body := map[string]any{
		"messaging_product": "whatsapp",
		"to":                phoneE164,
		"type":              "template",
		"template": map[string]any{
			"name":     s.template,
			"language": map[string]string{"code": s.lang},
			"components": []map[string]any{
				{"type": "body", "parameters": param},
				{"type": "button", "sub_type": "url", "index": "0", "parameters": param},
			},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("whatsapp marshal: %w", err)
	}
	url := fmt.Sprintf("%s/%s/messages", s.base, s.phoneID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("whatsapp request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("whatsapp send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("whatsapp send: status %d: %s", resp.StatusCode, detail)
	}
	return nil
}

// LogSender is the dev fallback when no WhatsApp credentials are configured: it
// logs the code so the flow is testable without a WABA. Never used outside dev.
type LogSender struct{ Log *slog.Logger }

func (s LogSender) SendCode(_ context.Context, phoneE164, code string) error {
	s.Log.Warn("DEV OTP (no WhatsApp configured)", "phone", phoneE164, "code", code)
	return nil
}
