package digest

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFormatDigestReferralSections(t *testing.T) {
	site := "https://adayisaholiday.com"
	unsub := site + "/unsubscribe?token=abc"
	day := time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
	holidays := []Holiday{
		{Name: "National Bologna Day", Category: "National"},
		{Name: "United Nations Day", Category: "International"},
		{Name: "Food & Fun Day", Category: "National"},
		{Name: "Extra Day", Category: "National"},
	}

	_, text, html := FormatDigest(day, holidays, site, unsub)

	for _, want := range []string{"Forward to a friend", "Did you get this forwarded?", "Sign up for free", "Open the calendar", "Unsubscribe", "account.html?signup=1", "mailto:?subject="} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q", want)
		}
	}
	for _, want := range []string{"Did you get this forwarded?", SignupURL(site), "Unsubscribe: " + unsub} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q", want)
		}
	}
	if !strings.HasSuffix(html, "</body></html>") {
		t.Error("html not closed")
	}

	_, _, preview := FormatDigest(day, holidays, site, "")
	if strings.Contains(preview, "Unsubscribe") {
		t.Error("preview should not include unsubscribe link")
	}
}

func TestForwardMailto(t *testing.T) {
	site := "https://adayisaholiday.com"
	holidays := []Holiday{{Name: "A & B Day"}, {Name: "C"}, {Name: "D"}, {Name: "E"}}
	link := ForwardMailto("Saturday, October 24", holidays, site)
	if strings.Contains(link, "+") || strings.Contains(link, " ") {
		t.Fatalf("mailto must use %%20 for spaces: %s", link)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if !strings.Contains(q.Get("subject"), "Saturday, October 24") {
		t.Errorf("subject = %q", q.Get("subject"))
	}
	body := q.Get("body")
	for _, want := range []string{"A & B Day, C, D, and 1 more", SignupURL(site)} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %q", want, body)
		}
	}
}
