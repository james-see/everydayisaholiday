package digest

import (
	"strings"
	"testing"
	"time"
)

func TestFormatDigestIncludesReferralSection(t *testing.T) {
	local := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	holidays := []Holiday{
		{Name: "National Mole Day", Category: "Food", Country: "US"},
	}
	siteURL := "https://adayisaholiday.com"

	subject, text, html := FormatDigest(local, holidays, siteURL)

	if !strings.Contains(subject, "Friday, October 23") {
		t.Fatalf("subject missing date label: %q", subject)
	}

	signupURL := siteURL + "/account.html?tab=signup"
	for _, got := range []string{text, html} {
		if !strings.Contains(got, "Forward to a friend") {
			t.Fatalf("missing forward CTA in output:\n%s", got)
		}
		if !strings.Contains(got, signupURL) {
			t.Fatalf("missing signup URL %q in output:\n%s", signupURL, got)
		}
		if !strings.Contains(got, "Did you get this forwarded?") {
			t.Fatalf("missing forwarded signup copy in output:\n%s", got)
		}
	}

	if !strings.Contains(text, "mailto:?") {
		t.Fatalf("text missing mailto forward link:\n%s", text)
	}
	if !strings.Contains(html, `href="mailto:?`) {
		t.Fatalf("html missing mailto forward link:\n%s", html)
	}
	if !strings.Contains(html, "Open the calendar") {
		t.Fatalf("html must keep Open the calendar link:\n%s", html)
	}
	if !strings.Contains(text, "Browse the calendar: "+siteURL+"/") {
		t.Fatalf("text must keep calendar link:\n%s", text)
	}
	if !strings.Contains(html, `background:#1a1a2e`) {
		t.Fatalf("html should keep dark card background:\n%s", html)
	}
	if !strings.Contains(html, `color:#e94560`) {
		t.Fatalf("html should keep accent link color:\n%s", html)
	}
}

func TestFormatDigestEmptyHolidaysStillHasReferral(t *testing.T) {
	local := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	_, text, html := FormatDigest(local, nil, "https://adayisaholiday.com")

	if !strings.Contains(text, "No holidays matched your filters today.") {
		t.Fatalf("expected empty-day copy in text:\n%s", text)
	}
	if !strings.Contains(html, "Sign up for free") {
		t.Fatalf("expected signup CTA even with no holidays:\n%s", html)
	}
}

func TestForwardMailto(t *testing.T) {
	got := forwardMailto("https://adayisaholiday.com", "Friday, October 23")
	if !strings.HasPrefix(got, "mailto:?") {
		t.Fatalf("expected mailto prefix, got %q", got)
	}
	if !strings.Contains(got, "subject=") || !strings.Contains(got, "body=") {
		t.Fatalf("expected subject and body query params, got %q", got)
	}
	if !strings.Contains(got, "adayisaholiday.com") {
		t.Fatalf("expected site URL in mailto body, got %q", got)
	}
}
