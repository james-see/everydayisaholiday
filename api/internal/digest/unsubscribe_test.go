package digest

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/james-see/everydayisaholiday/api/internal/db"
)

const testToken = "tok123"

func setupUnsub(t *testing.T) (*gin.Engine, *sql.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	now := "2026-10-25T00:00:00Z"
	if _, err := conn.Exec(`INSERT INTO users (id, email, password_hash, email_verified_at, created_at, updated_at) VALUES (1, 'a@example.com', 'x', ?, ?, ?)`, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO email_prefs (user_id, enabled, unsub_token, updated_at) VALUES (1, 1, ?, ?)`, testToken, now); err != nil {
		t.Fatal(err)
	}
	h := &Handler{DB: conn}
	r := gin.New()
	r.GET("/unsubscribe", h.UnsubscribeConfirm)
	r.POST("/unsubscribe", h.Unsubscribe)
	return r, conn
}

func enabledFor(t *testing.T, conn *sql.DB) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(`SELECT enabled FROM email_prefs WHERE user_id = 1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func do(r *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestUnsubscribeGETOnlyConfirms(t *testing.T) {
	r, conn := setupUnsub(t)
	w := do(r, httptest.NewRequest(http.MethodGet, "/unsubscribe?token="+testToken, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{`method="post"`, `action="/unsubscribe?token=` + testToken + `"`, "Got this email from a friend?", "account.html?signup=1"} {
		if !strings.Contains(body, want) {
			t.Errorf("confirm page missing %q", want)
		}
	}
	if enabledFor(t, conn) != 1 {
		t.Fatal("GET must not unsubscribe")
	}
}

func TestUnsubscribeGETInvalidToken(t *testing.T) {
	r, _ := setupUnsub(t)
	for _, path := range []string{"/unsubscribe?token=nope", "/unsubscribe"} {
		w := do(r, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "Link not valid") {
			t.Errorf("%s: status=%d body=%q", path, w.Code, w.Body.String())
		}
	}
}

func TestUnsubscribeFormPOST(t *testing.T) {
	r, conn := setupUnsub(t)
	req := httptest.NewRequest(http.MethodPost, "/unsubscribe?token="+testToken, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	w := do(r, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Unsubscribed") {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	if enabledFor(t, conn) != 0 {
		t.Fatal("POST should unsubscribe")
	}
	w = do(r, httptest.NewRequest(http.MethodGet, "/unsubscribe?token="+testToken, nil))
	if !strings.Contains(w.Body.String(), "Already unsubscribed") {
		t.Errorf("expected already-unsubscribed page, got %q", w.Body.String())
	}
}

func TestUnsubscribeRFC8058OneClick(t *testing.T) {
	r, conn := setupUnsub(t)
	req := httptest.NewRequest(http.MethodPost, "/unsubscribe?token="+testToken, strings.NewReader("List-Unsubscribe=One-Click"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := do(r, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	if enabledFor(t, conn) != 0 {
		t.Fatal("one-click POST should unsubscribe")
	}
}
