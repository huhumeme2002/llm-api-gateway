package logx

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
)

func New(logPrompts bool) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			k := strings.ToLower(a.Key)
			if strings.Contains(k, "authorization") || strings.Contains(k, "api_key") || strings.Contains(k, "apikey") ||
				strings.Contains(k, "proxy") || strings.Contains(k, "password") || strings.Contains(k, "secret") {
				return slog.String(a.Key, "[redacted]")
			}
			if !logPrompts && (k == "prompt" || k == "messages" || k == "body") {
				return slog.String(a.Key, "[omitted]")
			}
			return a
		},
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}

func Discard() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func Fields(v any) slog.Attr {
	b, err := json.Marshal(v)
	if err != nil {
		return slog.String("fields", err.Error())
	}
	return slog.String("fields", string(b))
}
