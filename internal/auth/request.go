package auth

import (
	"net/http"
	"strings"
)

func ClientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.TrimSpace(strings.SplitN(ip, ",", 2)[0])
	}
	ip := r.RemoteAddr
	if i := strings.LastIndex(ip, ":"); i != -1 {
		ip = ip[:i]
	}
	return strings.Trim(ip, "[]")
}

func UserAgent(r *http.Request) string {
	ua := r.UserAgent()
	if len(ua) > 512 {
		return ua[:512]
	}
	return ua
}
