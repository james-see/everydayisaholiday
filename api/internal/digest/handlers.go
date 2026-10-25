package digest

import (
	"database/sql"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/james-see/everydayisaholiday/api/internal/auth"
	"github.com/james-see/everydayisaholiday/api/internal/config"
	"github.com/james-see/everydayisaholiday/api/internal/mail"
	"github.com/james-see/everydayisaholiday/api/internal/timezone"
)

type Handler struct {
	DB       *sql.DB
	Cfg      config.Config
	Auth     *auth.Handler
	Mailer   mail.Sender
	Holidays *Store
	Runner   *Runner
}

type prefsDTO struct {
	Enabled    bool     `json:"enabled"`
	Timezone   string   `json:"timezone"`
	Categories []string `json:"categories"`
}

type prefsUpdate struct {
	Enabled    *bool    `json:"enabled"`
	Timezone   *string  `json:"timezone"`
	Categories *[]string `json:"categories"`
}

// GetPrefs godoc
// @Summary Get daily digest email preferences
// @Tags digest
// @Produce json
// @Success 200 {object} prefsDTO
// @Failure 401 {object} map[string]string
// @Router /auth/email-prefs [get]
func (h *Handler) GetPrefs(c *gin.Context) {
	uid, _, ok := h.Auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	p, err := h.ensurePrefs(uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load prefs"})
		return
	}
	c.JSON(http.StatusOK, p)
}

// PutPrefs godoc
// @Summary Update daily digest email preferences
// @Tags digest
// @Accept json
// @Produce json
// @Param body body prefsUpdate true "prefs"
// @Success 200 {object} prefsDTO
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /auth/email-prefs [put]
func (h *Handler) PutPrefs(c *gin.Context) {
	uid, _, ok := h.Auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	var req prefsUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	cur, err := h.ensurePrefs(uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load prefs"})
		return
	}
	enabled := cur.Enabled
	tz := cur.Timezone
	cats := cur.Categories
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.Timezone != nil {
		tz = strings.TrimSpace(*req.Timezone)
		if !timezone.Valid(tz) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid IANA timezone"})
			return
		}
	}
	if req.Categories != nil {
		cats = *req.Categories
		if cats == nil {
			cats = []string{}
		}
	}
	catsJSON, _ := json.Marshal(cats)
	now := time.Now().UTC().Format(time.RFC3339)
	en := 0
	if enabled {
		en = 1
	}
	_, err = h.DB.Exec(`
UPDATE email_prefs
SET enabled = ?, timezone = ?, categories = ?, updated_at = ?
WHERE user_id = ?`, en, tz, string(catsJSON), now, uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save prefs"})
		return
	}
	c.JSON(http.StatusOK, prefsDTO{Enabled: enabled, Timezone: tz, Categories: cats})
}

// Categories godoc
// @Summary List holiday categories for digest filters
// @Tags digest
// @Produce json
// @Success 200 {object} map[string]any
// @Router /auth/email-prefs/categories [get]
func (h *Handler) Categories(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"categories": h.Holidays.Categories()})
}

const pageStyle = `font-family:Georgia,serif;background:#1a1a2e;color:#e0e0e0;padding:2rem;text-align:center`

func unsubPage(c *gin.Context, status int, body string) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Robots-Tag", "noindex")
	c.String(status, `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Unsubscribe — A Day Is a Holiday</title></head><body style="`+pageStyle+`">`+body+`</body></html>`)
}

// UnsubscribeConfirm godoc
// @Summary Unsubscribe confirmation page (does not unsubscribe)
// @Description The email footer link lands here so a forwarded copy cannot unsubscribe the original recipient with a single click. Confirming POSTs to /unsubscribe.
// @Tags digest
// @Produce html
// @Param token query string true "unsubscribe token"
// @Success 200 {string} string "confirmation page"
// @Failure 400 {string} string "invalid token"
// @Router /unsubscribe [get]
func (h *Handler) UnsubscribeConfirm(c *gin.Context) {
	token := strings.TrimSpace(c.Query("token"))
	var enabled int
	err := sql.ErrNoRows
	if token != "" {
		err = h.DB.QueryRow(`SELECT enabled FROM email_prefs WHERE unsub_token = ?`, token).Scan(&enabled)
	}
	if err == sql.ErrNoRows {
		unsubPage(c, http.StatusBadRequest, `<h1 style="color:#e94560">Link not valid</h1>
<p>This unsubscribe link is invalid or has expired. You can manage emails from your <a href="/account.html" style="color:#e94560">account</a>.</p>`)
		return
	}
	if err != nil {
		unsubPage(c, http.StatusInternalServerError, `<h1 style="color:#e94560">Something went wrong</h1><p>Please try again later.</p>`)
		return
	}
	if enabled == 0 {
		unsubPage(c, http.StatusOK, `<h1 style="color:#e94560">Already unsubscribed</h1>
<p>This address no longer receives the daily holiday digest.</p>
<p><a href="/" style="color:#e94560">Back to calendar</a></p>`)
		return
	}
	action := "/unsubscribe?token=" + url.QueryEscape(token)
	unsubPage(c, http.StatusOK, `<h1 style="color:#e94560">Unsubscribe from the daily digest?</h1>
<p>Press the button to stop receiving the daily holiday email at the address it was sent to.</p>
<form method="post" action="`+html.EscapeString(action)+`" style="margin:1.5rem 0">
<button type="submit" style="font-family:Georgia,serif;font-size:1rem;background:#e94560;color:#fff;border:0;border-radius:6px;padding:.7rem 1.4rem;cursor:pointer">Unsubscribe</button>
</form>
<p style="color:#8a8a9a">Got this email from a friend? You don't need to do anything here — <a href="/account.html?signup=1" style="color:#e94560">sign up for your own free daily</a> instead.</p>
<p><a href="/" style="color:#e94560">Back to calendar</a></p>`)
}

// Unsubscribe godoc
// @Summary Unsubscribe from daily digest
// @Description Handles the confirmation form and RFC 8058 one-click POSTs (List-Unsubscribe=One-Click).
// @Tags digest
// @Produce json
// @Param token query string true "unsubscribe token"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /unsubscribe [post]
func (h *Handler) Unsubscribe(c *gin.Context) {
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		// RFC 8058 one-click may POST with body List-Unsubscribe=One-Click
		_ = c.Request.ParseForm()
		token = strings.TrimSpace(c.PostForm("token"))
		if token == "" {
			token = strings.TrimSpace(c.Query("token"))
		}
	}
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing token"})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := h.DB.Exec(`
UPDATE email_prefs SET enabled = 0, updated_at = ? WHERE unsub_token = ?`, now, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not unsubscribe"})
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token"})
		return
	}
	accept := c.GetHeader("Accept")
	if strings.Contains(accept, "text/html") || c.Query("format") == "html" {
		unsubPage(c, http.StatusOK, `<h1 style="color:#e94560">Unsubscribed</h1>
<p>You will no longer receive the daily holiday digest.</p>
<p><a href="/" style="color:#e94560">Back to calendar</a></p>`)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "unsubscribed"})
}

// RunNow godoc
// @Summary Trigger digest run (admin)
// @Tags digest
// @Produce json
// @Param X-Digest-Token header string false "admin token"
// @Success 200 {object} map[string]any
// @Router /internal/digest/run [post]
func (h *Handler) RunNow(c *gin.Context) {
	if h.Cfg.DigestAdminToken == "" || c.GetHeader("X-Digest-Token") != h.Cfg.DigestAdminToken {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	n, err := h.Runner.RunOnce(time.Now().UTC())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sent": n})
}

// Preview godoc
// @Summary Preview today's digest for the signed-in user
// @Tags digest
// @Produce json
// @Success 200 {object} map[string]any
// @Router /auth/email-prefs/preview [post]
func (h *Handler) Preview(c *gin.Context) {
	uid, email, ok := h.Auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	p, err := h.ensurePrefs(uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load prefs"})
		return
	}
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		loc = time.UTC
	}
	local := time.Now().In(loc)
	holidays := h.Holidays.ForDate(int(local.Month()), local.Day(), p.Categories)
	subject, text, _ := FormatDigest(local, holidays, h.Cfg.PublicBaseURL, "")
	c.JSON(http.StatusOK, gin.H{
		"email":       email,
		"local_date":  local.Format("2006-01-02"),
		"subject":     subject,
		"text":        text,
		"count":       len(holidays),
		"timezone":    p.Timezone,
	})
}

func (h *Handler) ensurePrefs(userID int64) (prefsDTO, error) {
	var (
		enabled  int
		tz       string
		catsJSON string
	)
	err := h.DB.QueryRow(`
SELECT enabled, timezone, categories FROM email_prefs WHERE user_id = ?`, userID).
		Scan(&enabled, &tz, &catsJSON)
	if err == sql.ErrNoRows {
		raw, _, err := auth.NewToken()
		if err != nil {
			return prefsDTO{}, err
		}
		now := time.Now().UTC().Format(time.RFC3339)
		_, err = h.DB.Exec(`
INSERT INTO email_prefs (user_id, enabled, timezone, categories, unsub_token, last_sent_local_date, updated_at)
VALUES (?, 0, 'UTC', '[]', ?, NULL, ?)`, userID, raw, now)
		if err != nil {
			return prefsDTO{}, err
		}
		return prefsDTO{Enabled: false, Timezone: "UTC", Categories: []string{}}, nil
	}
	if err != nil {
		return prefsDTO{}, err
	}
	var cats []string
	_ = json.Unmarshal([]byte(catsJSON), &cats)
	if cats == nil {
		cats = []string{}
	}
	return prefsDTO{Enabled: enabled == 1, Timezone: tz, Categories: cats}, nil
}
