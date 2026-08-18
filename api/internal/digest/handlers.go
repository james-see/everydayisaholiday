package digest

import (
	"database/sql"
	"encoding/json"
	"net/http"
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

// Unsubscribe godoc
// @Summary One-click unsubscribe from daily digest
// @Tags digest
// @Produce json
// @Param token query string true "unsubscribe token"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /unsubscribe [get]
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
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, `<!DOCTYPE html><html><body style="font-family:Georgia,serif;background:#1a1a2e;color:#e0e0e0;padding:2rem;text-align:center">
<h1 style="color:#e94560">Unsubscribed</h1>
<p>You will no longer receive the daily holiday digest.</p>
<p><a href="/" style="color:#e94560">Back to calendar</a></p>
</body></html>`)
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
	subject, text, _ := FormatDigest(local, holidays, h.Cfg.PublicBaseURL)
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
