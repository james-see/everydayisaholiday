package digest

import (
	"encoding/json"
	"fmt"
	"net/url"
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

// SignupURL is the free account/digest signup page.
func SignupURL(siteURL string) string {
	return siteURL + "/account.html?signup=1&utm_source=digest&utm_medium=email&utm_campaign=forwarded"
}

// ForwardMailto builds a mailto: link prefilled with today's highlights and a signup link.
func ForwardMailto(label string, holidays []Holiday, siteURL string) string {
	subject := "Every day is a holiday — " + label
	var b strings.Builder
	b.WriteString("Thought you'd enjoy this: today (" + label + ") is")
	if len(holidays) == 0 {
		b.WriteString(" a holiday somewhere.")
	} else {
		n := len(holidays)
		if n > 3 {
			n = 3
		}
		names := make([]string, 0, n)
		for _, h := range holidays[:n] {
			names = append(names, h.Name)
		}
		b.WriteString(" " + strings.Join(names, ", "))
		if len(holidays) > n {
			fmt.Fprintf(&b, ", and %d more", len(holidays)-n)
		}
		b.WriteString(".")
	}
	b.WriteString("\n\nGet your own free daily holiday email: " + SignupURL(siteURL) + "\n")
	return "mailto:?subject=" + mailtoEscape(subject) + "&body=" + mailtoEscape(b.String())
}

func mailtoEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// FormatDigest renders the daily email. unsubURL may be empty (e.g. previews).
func FormatDigest(localDate time.Time, holidays []Holiday, siteURL, unsubURL string) (subject, text, html string) {
	label := localDate.Format("Monday, January 2")
	subject = "Today's holidays — " + label
	signup := SignupURL(siteURL)
	forward := ForwardMailto(label, holidays, siteURL)

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
	b.WriteString("\n---\n")
	b.WriteString("Know someone who'd love this? Forward this email to a friend, or share: " + siteURL + "/\n")
	b.WriteString("Did you get this forwarded? Sign up for free to get your own daily in your inbox: " + signup + "\n")
	b.WriteString("---\n")
	b.WriteString("\nBrowse the calendar: " + siteURL + "/\n")
	if unsubURL != "" {
		b.WriteString("\nUnsubscribe: " + unsubURL + "\n")
	}
	text = b.String()

	const (
		bg     = "#1a1a2e"
		card   = "#22223b"
		accent = "#e94560"
		fg     = "#e0e0e0"
		muted  = "#8a8a9a"
		font   = "Georgia,'Times New Roman',serif"
	)
	var hb strings.Builder
	hb.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="dark"><meta name="supported-color-schemes" content="dark"></head>`)
	fmt.Fprintf(&hb, `<body style="margin:0;padding:0;background:%s;color:%s;font-family:%s">`, bg, fg, font)
	fmt.Fprintf(&hb, `<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" bgcolor="%s" style="background:%s"><tr><td align="center" style="padding:24px 12px">`, bg, bg)
	hb.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="max-width:600px">`)

	fmt.Fprintf(&hb, `<tr><td style="padding:0 0 16px;font-family:%s;font-size:13px;color:%s;text-align:center">Got this from a friend? <a href="%s" style="color:%s;text-decoration:underline">Get your own free daily</a> · <a href="%s" style="color:%s;text-decoration:underline">Forward to a friend</a></td></tr>`,
		font, muted, escapeHTML(signup), accent, escapeHTML(forward), accent)

	fmt.Fprintf(&hb, `<tr><td style="padding:0 8px;font-family:%s;color:%s">`, font, fg)
	fmt.Fprintf(&hb, `<h1 style="margin:0 0 8px;font-family:%s;font-size:28px;color:%s">A Day Is a Holiday</h1><p style="margin:0 0 16px;font-size:16px;color:%s">%s</p>`, font, accent, fg, escapeHTML(label))
	if len(holidays) == 0 {
		hb.WriteString(`<p style="font-size:16px">No holidays matched your filters today.</p>`)
	} else {
		hb.WriteString(`<ul style="margin:0 0 16px;padding-left:20px;font-size:16px;line-height:1.5">`)
		for _, h := range holidays {
			fmt.Fprintf(&hb, "<li><strong>%s</strong> — %s", escapeHTML(h.Name), escapeHTML(h.Category))
			if h.Country != "" {
				fmt.Fprintf(&hb, " (%s)", escapeHTML(h.Country))
			}
			hb.WriteString("</li>")
		}
		hb.WriteString("</ul>")
	}
	hb.WriteString(`</td></tr>`)

	fmt.Fprintf(&hb, `<tr><td style="padding:16px 0"><table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" bgcolor="%s" style="background:%s;border:1px solid %s;border-radius:8px"><tr><td style="padding:20px;font-family:%s;color:%s">`, card, card, accent, font, fg)
	fmt.Fprintf(&hb, `<p style="margin:0 0 8px;font-size:18px;color:%s"><strong>Know someone who'd love this?</strong></p>`, accent)
	fmt.Fprintf(&hb, `<p style="margin:0 0 16px;font-size:15px;line-height:1.5">Forward today's email to a friend, or share <a href="%s/" style="color:%s">adayisaholiday.com</a>.</p>`, siteURL, accent)
	fmt.Fprintf(&hb, `<table role="presentation" cellpadding="0" cellspacing="0" border="0"><tr><td bgcolor="%s" style="background:%s;border-radius:6px"><a href="%s" style="display:inline-block;padding:10px 18px;font-family:%s;font-size:15px;color:#ffffff;text-decoration:none">Forward to a friend</a></td></tr></table>`,
		accent, accent, escapeHTML(forward), font)
	fmt.Fprintf(&hb, `<p style="margin:20px 0 0;padding-top:16px;border-top:1px solid #3a3a55;font-size:15px;line-height:1.5"><strong>Did you get this forwarded?</strong> <a href="%s" style="color:%s">Sign up for free</a> to get your own daily in your inbox.</p>`,
		escapeHTML(signup), accent)
	hb.WriteString(`</td></tr></table></td></tr>`)

	fmt.Fprintf(&hb, `<tr><td style="padding:8px;font-family:%s;font-size:12px;color:%s;text-align:center">`, font, muted)
	fmt.Fprintf(&hb, `<a href="%s/" style="color:%s">Open the calendar</a>`, siteURL, accent)
	if unsubURL != "" {
		fmt.Fprintf(&hb, ` · <a href="%s" style="color:%s">Unsubscribe</a>`, escapeHTML(unsubURL), muted)
	}
	hb.WriteString(`</td></tr></table></td></tr></table></body></html>`)
	html = hb.String()
	return subject, text, html
}

func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
