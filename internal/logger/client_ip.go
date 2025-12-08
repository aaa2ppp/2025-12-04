package logger

import (
	"net"
	"net/http"
	"strings"
)

// getClientIP извлекает реальный (или максимально, что сможет) IP адрес клиента из запроса.
func getClientIP(r *http.Request) string {
	// Задан явно в X-Real-IP
	clientIP := r.Header.Get("X-Real-IP")
	if clientIP != "" {
		return clientIP
	}

	// Первый IP в цепочке X-Forwarded-For
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		p := strings.IndexByte(xff, ',')
		if p == -1 {
			p = len(xff)
		}
		clientIP = strings.TrimSpace(xff[:p])
		return clientIP
	}

	// fallback на RemoteAddr
	clientIP = r.RemoteAddr
	if host, _, err := net.SplitHostPort(clientIP); err == nil {
		clientIP = host
	}
	return clientIP
}
