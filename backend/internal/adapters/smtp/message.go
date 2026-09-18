package smtpadapter

import (
	"mime"
	"strings"
)

func BuildMessage(from, recipient, subject, body, messageID string) string {
	subject = strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(subject))
	encodedSubject := mime.QEncoding.Encode("UTF-8", subject)
	return "From: " + from + "\r\n" +
		"To: " + recipient + "\r\n" +
		"Subject: " + encodedSubject + "\r\n" +
		"Message-ID: " + messageID + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" + body
}
