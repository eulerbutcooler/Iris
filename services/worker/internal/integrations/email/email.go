package email

import (
	"context"
	"encoding/json"
	"fmt"
	"net/smtp"
	"os"
	"strings"

	"github.com/eulerbutcooler/iris/services/worker/internal/engine"
)

// Executor implements engine.ActionExecutor for "email_send".
// Uses SMTP with credentials from environment variables:
//
//	SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASS, SMTP_FROM
type Executor struct {
	host string
	port string
	user string
	pass string
	from string
}

// New creates an email_send executor reading SMTP config from env.
func New() *Executor {
	return &Executor{
		host: getEnv("SMTP_HOST", ""),
		port: getEnv("SMTP_PORT", "587"),
		user: getEnv("SMTP_USER", ""),
		pass: getEnv("SMTP_PASS", ""),
		from: getEnv("SMTP_FROM", ""),
	}
}

func (e *Executor) Execute(
	ctx context.Context,
	config map[string]any,
	payload []byte,
	prevOutputs map[string]engine.StepOutput,
) (json.RawMessage, error) {
	to, _ := config["to"].(string)
	subject, _ := config["subject"].(string)
	body, _ := config["body"].(string)

	if to == "" {
		return nil, fmt.Errorf("email_send: to is required")
	}
	if subject == "" {
		return nil, fmt.Errorf("email_send: subject is required")
	}
	if e.host == "" {
		return nil, fmt.Errorf("email_send: SMTP_HOST is not configured")
	}

	addr := e.host + ":" + e.port
	auth := smtp.PlainAuth("", e.user, e.pass, e.host)

	msg := buildMIME(e.from, to, subject, body)

	if err := smtp.SendMail(addr, auth, e.from, []string{to}, []byte(msg)); err != nil {
		return nil, fmt.Errorf("email_send: smtp: %w", err)
	}

	out, _ := json.Marshal(map[string]any{"sent": true, "to": to})
	return json.RawMessage(out), nil
}

// buildMIME constructs a minimal MIME email message.
func buildMIME(from, to, subject, body string) string {
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)
	return sb.String()
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
