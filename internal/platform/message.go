package platform

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const serverMessageHeader = "X-Hoshilet-Server-Message"

// The limit is on UTF-8 message bytes, before transport encoding (<=684 bytes).
func validateServerMessage(message string) (string, error) {
	message = strings.ReplaceAll(message, "\r\n", "\n")
	if !utf8.ValidString(message) || len(message) > 512 {
		return "", errors.New("server_message must be valid UTF-8 and at most 512 bytes")
	}
	for _, r := range message {
		if r != '\n' && unicode.IsControl(r) {
			return "", errors.New("server_message allows plain text and LF only; terminal/control characters are forbidden")
		}
	}
	return message, nil
}

type messageWriter struct {
	http.ResponseWriter
	encoded string
	written bool
}

func (w *messageWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *messageWriter) WriteHeader(status int) {
	if w.written {
		return
	}
	w.Header().Del(serverMessageHeader)
	if status >= 200 && status < 300 && w.encoded != "" {
		w.Header().Set(serverMessageHeader, w.encoded)
	}
	if status >= 200 || status == 101 {
		w.written = true
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *messageWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
func encodeServerMessage(message string) string {
	if message == "" {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(message))
}
