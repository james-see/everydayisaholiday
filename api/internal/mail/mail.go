package mail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type Sender interface {
	Send(to, subject, text string) error
}

type LogSender struct{}

func (LogSender) Send(to, subject, text string) error {
	log.Printf("mail[log] to=%s subject=%q body=%q", to, subject, text)
	return nil
}

type ResendSender struct {
	APIKey string
	From   string
	Client *http.Client
}

func (r ResendSender) Send(to, subject, text string) error {
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	body, err := json.Marshal(map[string]any{
		"from":    r.From,
		"to":      []string{to},
		"subject": subject,
		"text":    text,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("resend status %d", res.StatusCode)
	}
	return nil
}

func New(apiKey, from string) Sender {
	if apiKey == "" {
		return LogSender{}
	}
	return ResendSender{APIKey: apiKey, From: from}
}
