package digest

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type Holiday struct {
	Date     string `json:"date"`
	Month    int    `json:"month"`
	Day      int    `json:"day"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Country  string `json:"country,omitempty"`
}

type Store struct {
	mu   sync.RWMutex
	byMD map[string][]Holiday
	cats []string
	path string
}

func Load(path string) (*Store, error) {
	s := &Store{path: path, byMD: map[string][]Holiday{}}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Reload() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var list []Holiday
	if err := json.Unmarshal(b, &list); err != nil {
		return err
	}
	by := make(map[string][]Holiday, 366)
	catSet := map[string]struct{}{}
	for _, h := range list {
		key := h.Date
		if key == "" {
			key = fmt.Sprintf("%02d-%02d", h.Month, h.Day)
		}
		by[key] = append(by[key], h)
		if h.Category != "" {
			catSet[h.Category] = struct{}{}
		}
	}
	cats := make([]string, 0, len(catSet))
	for c := range catSet {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	s.mu.Lock()
	s.byMD = by
	s.cats = cats
	s.mu.Unlock()
	return nil
}

func (s *Store) Categories() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.cats))
	copy(out, s.cats)
	return out
}

func (s *Store) ForDate(month, day int, categories []string) []Holiday {
	key := fmt.Sprintf("%02d-%02d", month, day)
	s.mu.RLock()
	defer s.mu.RUnlock()
	src := s.byMD[key]
	if len(categories) == 0 {
		out := make([]Holiday, len(src))
		copy(out, src)
		return out
	}
	allow := map[string]struct{}{}
	for _, c := range categories {
		allow[c] = struct{}{}
	}
	out := make([]Holiday, 0, len(src))
	for _, h := range src {
		if _, ok := allow[h.Category]; ok {
			out = append(out, h)
		}
	}
	return out
}

// Search returns holidays matching q in name/country, optional category and date (MM-DD).
func (s *Store) Search(q, category, date string, limit int) []Holiday {
	if limit <= 0 {
		limit = 50
	}
	q = strings.ToLower(strings.TrimSpace(q))
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Holiday, 0, limit)
	appendMatch := func(list []Holiday) {
		for _, h := range list {
			if len(out) >= limit {
				return
			}
			if category != "" && h.Category != category {
				continue
			}
			if q != "" {
				hay := strings.ToLower(h.Name + " " + h.Country)
				if !strings.Contains(hay, q) {
					continue
				}
			}
			out = append(out, h)
		}
	}
	if date != "" {
		appendMatch(s.byMD[date])
		return out
	}
	keys := make([]string, 0, len(s.byMD))
	for k := range s.byMD {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		appendMatch(s.byMD[k])
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, list := range s.byMD {
		n += len(list)
	}
	return n
}

func FormatDigest(localDate time.Time, holidays []Holiday, siteURL string) (subject, text, html string) {
	label := localDate.Format("Monday, January 2")
	subject = "Today's holidays — " + label
	var b strings.Builder
	fmt.Fprintf(&b, "A Day Is a Holiday — %s\n\n", label)
	if len(holidays) == 0 {
		b.WriteString("No holidays matched your filters today.\n")
	} else {
		fmt.Fprintf(&b, "%d observances today:\n\n", len(holidays))
		for _, h := range holidays {
			line := "- " + h.Name + " [" + h.Category + "]"
			if h.Country != "" {
				line += " (" + h.Country + ")"
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\nBrowse the calendar: " + siteURL + "/\n")
	text = b.String()

	var hb strings.Builder
	hb.WriteString("<html><body style=\"font-family:Georgia,serif;background:#1a1a2e;color:#e0e0e0;padding:24px\">")
	fmt.Fprintf(&hb, "<h1 style=\"color:#e94560\">A Day Is a Holiday</h1><p>%s</p>", label)
	if len(holidays) == 0 {
		hb.WriteString("<p>No holidays matched your filters today.</p>")
	} else {
		hb.WriteString("<ul>")
		for _, h := range holidays {
			fmt.Fprintf(&hb, "<li><strong>%s</strong> — %s", escapeHTML(h.Name), escapeHTML(h.Category))
			if h.Country != "" {
				fmt.Fprintf(&hb, " (%s)", escapeHTML(h.Country))
			}
			hb.WriteString("</li>")
		}
		hb.WriteString("</ul>")
	}
	fmt.Fprintf(&hb, `<p><a href="%s/" style="color:#e94560">Open the calendar</a></p>`, siteURL)
	html = hb.String()
	return subject, text, html
}

func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
