package provider

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var (
	userinfoRe = regexp.MustCompile(`(?i)((?:https?|socks5h?|socks)://)[^/@\s:]+:[^/@\s]+@`)
	bearerRe   = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._\-+=/]{8,}`)
	keyRe      = regexp.MustCompile(`(?i)(sk-[A-Za-z0-9]{8,}|api[_-]?key[=:\s]+)[A-Za-z0-9._\-+=/]+`)
)

func Redact(s string) string {
	if s == "" {
		return s
	}
	s = userinfoRe.ReplaceAllString(s, "${1}***:***@")
	s = bearerRe.ReplaceAllString(s, "${1}[redacted]")
	s = keyRe.ReplaceAllString(s, "[redacted]")
	return s
}

func RedactError(err error) error {
	if err == nil {
		return nil
	}
	msg := Redact(err.Error())
	if msg == err.Error() {
		return err
	}
	return errors.New(msg)
}

func ProxyKind(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "none"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return "unknown"
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h", "socks":
		return strings.ToLower(u.Scheme)
	default:
		return u.Scheme
	}
}

func SanitizeProxyMeta(raw string) (kind string, hasAuth bool) {
	kind = ProxyKind(raw)
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return kind, false
	}
	return kind, u.User != nil
}
