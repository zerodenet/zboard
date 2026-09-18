package smtpadapter

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"net"
	"net/smtp"
	"strconv"
	"time"
)

type clientSession struct {
	*smtp.Client
	stop func() bool
}

func (s *clientSession) Close() error { s.stop(); return s.Client.Close() }
func openClient(ctx context.Context, settings platform.SMTPSettings) (*clientSession, error) {
	address := net.JoinHostPort(settings.Host, strconv.Itoa(settings.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	tlsConfig := &tls.Config{ServerName: settings.Host, MinVersion: tls.VersionTLS12}
	if settings.TLSMode == "implicit" {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, fmt.Errorf("connect SMTP server: %w", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	successful := false
	defer func() {
		if !successful {
			stop()
			_ = conn.Close()
		}
	}()
	client, err := smtp.NewClient(conn, settings.Host)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("create SMTP client: %w", err)
	}
	if settings.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			_ = client.Close()
			return nil, errors.New("SMTP server does not advertise STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if settings.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", settings.Username, settings.Password, settings.Host)); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("authenticate SMTP client: %w", err)
		}
	}
	successful = true
	return &clientSession{Client: client, stop: stop}, nil
}

func Check(ctx context.Context, settings platform.SMTPSettings) error {
	client, err := openClient(ctx, settings)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Noop(); err != nil {
		return fmt.Errorf("SMTP NOOP failed: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("quit SMTP session: %w", err)
	}
	return nil
}

func send(ctx context.Context, settings platform.SMTPSettings, recipient, subject, body, messageID string) error {
	client, err := openClient(ctx, settings)
	if err != nil {
		return err
	}
	defer client.Close()
	return submit(client, settings.From, recipient, BuildMessage(settings.From, recipient, subject, body, messageID))
}
