package mail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
	Headers map[string]string
}

type Sender interface {
	Send(to, subject, text string) error
	SendMessage(msg Message) error
}

type LogSender struct{}

func (LogSender) Send(to, subject, text string) error {
	return LogSender{}.SendMessage(Message{To: to, Subject: subject, Text: text})
}

func (LogSender) SendMessage(msg Message) error {
	log.Printf("mail[log] to=%s subject=%q headers=%v body=%q", msg.To, msg.Subject, msg.Headers, msg.Text)
	return nil
}

// MailjetSender sends via Mailjet API v3.1.
type MailjetSender struct {
	APIKey    string
	SecretKey string
	From      string
	Client    *http.Client
}

type mailjetPayload struct {
	Messages []mailjetMessage `json:"Messages"`
}

type mailjetMessage struct {
	From     mailjetAddress   `json:"From"`
	To       []mailjetAddress `json:"To"`
	Subject  string           `json:"Subject"`
	TextPart string           `json:"TextPart"`
	HTMLPart string           `json:"HTMLPart,omitempty"`
	Headers  map[string]string `json:"Headers,omitempty"`
}

type mailjetAddress struct {
	Email string `json:"Email"`
	Name  string `json:"Name,omitempty"`
}

func (m MailjetSender) Send(to, subject, text string) error {
	return m.SendMessage(Message{To: to, Subject: subject, Text: text})
}

func (m MailjetSender) SendMessage(msg Message) error {
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	fromEmail, fromName := splitFrom(m.From)
	if fromEmail == "" {
		return fmt.Errorf("mailjet: invalid MAIL_FROM")
	}
	mj := mailjetMessage{
		From:     mailjetAddress{Email: fromEmail, Name: fromName},
		To:       []mailjetAddress{{Email: msg.To}},
		Subject:  msg.Subject,
		TextPart: msg.Text,
		HTMLPart: msg.HTML,
		Headers:  msg.Headers,
	}
	body, err := json.Marshal(mailjetPayload{Messages: []mailjetMessage{mj}})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, "https://api.mailjet.com/v3.1/send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.SetBasicAuth(m.APIKey, m.SecretKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode >= 300 {
		return fmt.Errorf("mailjet status %d: %s", res.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func splitFrom(from string) (email, name string) {
	from = strings.TrimSpace(from)
	if from == "" {
		return "", ""
	}
	addr, err := mail.ParseAddress(from)
	if err != nil {
		if strings.Contains(from, "@") && !strings.ContainsAny(from, "<>") {
			return from, ""
		}
		return "", ""
	}
	return addr.Address, addr.Name
}

func New(apiKey, secretKey, from string) Sender {
	if apiKey == "" || secretKey == "" {
		return LogSender{}
	}
	return MailjetSender{APIKey: apiKey, SecretKey: secretKey, From: from}
}
