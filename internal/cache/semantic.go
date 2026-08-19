package cache

import "strings"

// Semantic is optional and disabled by default. Exact matching is never
// replaced by similarity. When enabled it only applies to allow-listed
// workloads that are not tool-calling / mutation loops.
type Semantic struct {
	Enabled bool
	Allowed []string
}

func (s Semantic) AllowedFor(workload string, hasTools bool) bool {
	if !s.Enabled {
		return false
	}
	if hasTools {
		return false
	}
	wl := strings.ToLower(strings.TrimSpace(workload))
	switch wl {
	case "tool-loop", "code-mod", "debug-runtime", "shell", "mutate", "":
		return false
	}
	if len(s.Allowed) == 0 {
		return false
	}
	for _, a := range s.Allowed {
		if strings.EqualFold(a, wl) {
			return true
		}
	}
	return false
}
