package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

const Schema = "v1"

type Request struct {
	SchemaVersion    string
	Tenant           string
	Protocol         string
	Provider         string
	Model            string
	Messages         json.RawMessage
	SystemPrompt     json.RawMessage
	DeveloperPrompt  json.RawMessage
	Temperature      *float64
	TopP             *float64
	MaxTokens        *int
	Seed             *int64
	ResponseFormat   json.RawMessage
	Tools            json.RawMessage
	ToolChoice       json.RawMessage
	Reasoning        json.RawMessage
	ProviderParams   json.RawMessage
	MultimodalHashes []string
	PreviousResponse string
	HasTools         bool
}

type PrefixInfo struct {
	Hash   string
	Length int
}

func StructuralJSON(b []byte) ([]byte, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil, nil
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return marshalStable(v)
}

func marshalStable(v any) ([]byte, error) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			vb, err := marshalStable(t[k])
			if err != nil {
				return nil, err
			}
			buf.Write(vb)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []any:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			vb, err := marshalStable(item)
			if err != nil {
				return nil, err
			}
			buf.Write(vb)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	case json.Number:
		return []byte(t.String()), nil
	default:
		return json.Marshal(t)
	}
}

func Hash(parts ...[]byte) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write(p)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (r Request) CacheKey(schemaVersion string) (string, string, error) {
	if schemaVersion == "" {
		schemaVersion = Schema
	}
	msg, err := StructuralJSON(r.Messages)
	if err != nil {
		return "", "", err
	}
	sys, err := StructuralJSON(r.SystemPrompt)
	if err != nil {
		return "", "", err
	}
	dev, err := StructuralJSON(r.DeveloperPrompt)
	if err != nil {
		return "", "", err
	}
	rf, err := StructuralJSON(r.ResponseFormat)
	if err != nil {
		return "", "", err
	}
	tools, err := StructuralJSON(r.Tools)
	if err != nil {
		return "", "", err
	}
	tc, err := StructuralJSON(r.ToolChoice)
	if err != nil {
		return "", "", err
	}
	rs, err := StructuralJSON(r.Reasoning)
	if err != nil {
		return "", "", err
	}
	pp, err := StructuralJSON(r.ProviderParams)
	if err != nil {
		return "", "", err
	}
	var buf bytes.Buffer
	write := func(s string) { buf.WriteString(s); buf.WriteByte('\n') }
	write("schema=" + schemaVersion)
	write("tenant=" + r.Tenant)
	write("protocol=" + r.Protocol)
	write("provider=" + r.Provider)
	write("model=" + r.Model)
	write("messages=" + string(msg))
	write("system=" + string(sys))
	write("developer=" + string(dev))
	write("temp=" + fmtFloat(r.Temperature))
	write("top_p=" + fmtFloat(r.TopP))
	write("max_tokens=" + fmtInt(r.MaxTokens))
	write("seed=" + fmtInt64(r.Seed))
	write("response_format=" + string(rf))
	write("tools=" + string(tools))
	write("tool_choice=" + string(tc))
	write("reasoning=" + string(rs))
	write("provider_params=" + string(pp))
	write("mm=" + strings.Join(r.MultimodalHashes, ","))
	write("prev=" + r.PreviousResponse)
	sum := Hash(buf.Bytes())
	key := fmt.Sprintf("llmgw:%s:%s:resp:%s", schemaVersion, r.Tenant, sum)
	return key, sum, nil
}

func Prefix(messages json.RawMessage, tools json.RawMessage, system json.RawMessage) PrefixInfo {
	arr := gjson.ParseBytes(messages)
	var prefix bytes.Buffer
	prefix.Write(system)
	prefix.WriteByte(0)
	prefix.Write(tools)
	prefix.WriteByte(0)
	if arr.IsArray() {
		items := arr.Array()
		end := len(items) - 1
		if end < 0 {
			end = 0
		}
		for i := 0; i < end; i++ {
			prefix.WriteString(items[i].Raw)
			prefix.WriteByte(0)
		}
	} else {
		prefix.Write(messages)
	}
	return PrefixInfo{Hash: Hash(prefix.Bytes()), Length: prefix.Len()}
}

func MultimodalHashes(messages json.RawMessage) []string {
	var out []string
	gjson.ParseBytes(messages).ForEach(func(_, msg gjson.Result) bool {
		content := msg.Get("content")
		if content.IsArray() {
			content.ForEach(func(_, part gjson.Result) bool {
				switch part.Get("type").String() {
				case "image_url", "image", "input_image":
					src := part.Get("image_url.url").String()
					if src == "" {
						src = part.Get("source.data").String()
					}
					if src == "" {
						src = part.Raw
					}
					out = append(out, Hash([]byte(src)))
				}
				return true
			})
		}
		return true
	})
	return out
}

func fmtFloat(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func fmtInt(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func fmtInt64(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}
