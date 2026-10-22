// Package mailer 负责发送事务邮件。
//
// V1.0 只有两种驱动：console（本地与测试，把邮件打到日志）与 smtp（生产）。
// 不引入第三方邮件 SDK：注册验证与找回密码只需要纯文本邮件。
package mailer

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"time"

	"go-blog/internal/config"
)

// Message 是一封纯文本邮件。
type Message struct {
	To      string
	Subject string
	Text    string
}

// Mailer 是邮件发送接口，便于测试替换。
type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// New 根据配置返回具体驱动。
func New(cfg *config.Config, log *slog.Logger) (Mailer, error) {
	switch cfg.Mail.Driver {
	case "console":
		return &consoleMailer{log: log}, nil
	case "smtp":
		return &smtpMailer{cfg: cfg.Mail, log: log}, nil
	default:
		return nil, fmt.Errorf("不支持的邮件驱动: %q", cfg.Mail.Driver)
	}
}

// consoleMailer 把邮件内容写进结构化日志。
//
// 生产环境禁止使用：日志会被采集，令牌随之泄露。
// config.Validate 已在生产环境强制要求 smtp 或显式配置。
type consoleMailer struct {
	log *slog.Logger
}

func (m *consoleMailer) Send(_ context.Context, msg Message) error {
	m.log.Info("mail_console",
		slog.String("to", msg.To),
		slog.String("subject", msg.Subject),
		slog.String("body", msg.Text),
	)
	return nil
}

// smtpMailer 通过 SMTP 发送。支持 STARTTLS 与隐式 TLS（465）。
type smtpMailer struct {
	cfg config.Mail
	log *slog.Logger
}

func (m *smtpMailer) Send(ctx context.Context, msg Message) error {
	addr := net.JoinHostPort(m.cfg.SMTPHost, fmt.Sprint(m.cfg.SMTPPort))

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("连接邮件服务器失败: %w", err)
	}
	defer conn.Close()

	// 465 端口走隐式 TLS，其余端口走 STARTTLS
	if m.cfg.SMTPPort == 465 {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: m.cfg.SMTPHost, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("TLS 握手失败: %w", err)
		}
		conn = tlsConn
	}

	client, err := smtp.NewClient(conn, m.cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("创建 SMTP 客户端失败: %w", err)
	}
	defer client.Close()

	if m.cfg.SMTPPort != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: m.cfg.SMTPHost, MinVersion: tls.VersionTLS12}); err != nil {
				return fmt.Errorf("STARTTLS 失败: %w", err)
			}
		}
	}

	if m.cfg.SMTPUser != "" {
		if err := client.Auth(smtp.PlainAuth("", m.cfg.SMTPUser, m.cfg.SMTPPass, m.cfg.SMTPHost)); err != nil {
			return fmt.Errorf("SMTP 认证失败: %w", err)
		}
	}

	if err := client.Mail(m.cfg.From); err != nil {
		return fmt.Errorf("设置发件人失败: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("设置收件人失败: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("打开数据通道失败: %w", err)
	}
	if _, err := w.Write(buildBody(m.cfg.From, msg)); err != nil {
		return fmt.Errorf("写入邮件内容失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("提交邮件内容失败: %w", err)
	}
	return client.Quit()
}

// buildBody 组装最简 RFC 5322 文本邮件。
// 主题按 RFC 2047 编码，避免中文主题在部分客户端乱码。
func buildBody(from string, msg Message) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + msg.To + "\r\n")
	b.WriteString("Subject: " + encodeSubject(msg.Subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(msg.Text)
	b.WriteString("\r\n")
	return []byte(b.String())
}

func encodeSubject(s string) string {
	return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
}
