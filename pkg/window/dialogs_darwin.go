//go:build darwin

package window

import (
	"path/filepath"
	"strings"

	"github.com/scotticad/scotticad/internal/mac"
)

type filterSpec struct {
	name     string
	patterns []string
}

func parseFilters(filter []string) []filterSpec {
	var out []filterSpec
	for i := 0; i+1 < len(filter); i += 2 {
		var patterns []string
		for _, p := range strings.Split(filter[i+1], ";") {
			if p = strings.TrimSpace(p); p != "" {
				patterns = append(patterns, p)
			}
		}
		out = append(out, filterSpec{name: filter[i], patterns: patterns})
	}
	return out
}

func encodeFilters(filters []filterSpec) string {
	lines := make([]string, 0, len(filters))
	for _, f := range filters {
		if len(f.patterns) > 0 {
			lines = append(lines, f.name+"|"+strings.Join(f.patterns, ";"))
		}
	}
	return strings.Join(lines, "\n")
}

func (w *Window) OpenFile(title string, filter ...string) (string, bool) {
	return mac.OpenFile(title, encodeFilters(parseFilters(filter)))
}

func (w *Window) SaveFile(title, initial, defExt string, filter ...string) (string, bool) {
	name := ""
	if initial != "" {
		name = filepath.Base(initial)
		if name == "." || name == string(filepath.Separator) {
			name = ""
		}
	}
	path, ok := mac.SaveFile(title, encodeFilters(parseFilters(filter)), name)
	if ok && defExt != "" && filepath.Ext(path) == "" {
		path += "." + strings.TrimPrefix(defExt, ".")
	}
	return path, ok
}

func (w *Window) Message(title, text string, kind int) int {
	return mac.Message(title, text, 0, kind)
}

func ErrorBox(title, text string) {
	mac.Message(title, text, 2, MsgOK)
}

func OpenURL(url string) { mac.OpenURL(url) }
