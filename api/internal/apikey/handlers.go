package apikey

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/james-see/everydayisaholiday/api/internal/auth"
	"github.com/james-see/everydayisaholiday/api/internal/bearer"
)

type Handler struct {
	DB   *sql.DB
	Auth *auth.Handler
}

type createReq struct {
	Name string `json:"name"`
}

type keyDTO struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Prefix    string  `json:"prefix"`
	RateTier string  `json:"rate_tier"`
	CreatedAt string  `json:"created_at"`
	RevokedAt *string `json:"revoked_at,omitempty"`
	LastUsed  *string `json:"last_used_at,omitempty"`
	Key       string  `json:"key,omitempty"` // only on create
}

// List godoc
// @Summary List API keys
// @Tags apikeys
// @Produce json
// @Success 200 {object} map[string]any
// @Router /auth/api-keys [get]
func (h *Handler) List(c *gin.Context) {
	uid, _, ok := h.Auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	rows, err := h.DB.Query(`
SELECT id, name, prefix, rate_tier, created_at, revoked_at, last_used_at
FROM api_keys WHERE user_id = ? ORDER BY id DESC`, uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list keys"})
		return
	}
	defer rows.Close()
	out := []keyDTO{}
	for rows.Next() {
		var k keyDTO
		var revoked, last sql.NullString
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.RateTier, &k.CreatedAt, &revoked, &last); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list keys"})
			return
		}
		if revoked.Valid {
			k.RevokedAt = &revoked.String
		}
		if last.Valid {
			k.LastUsed = &last.String
		}
		out = append(out, k)
	}
	c.JSON(http.StatusOK, gin.H{"keys": out})
}

// Create godoc
// @Summary Create API key (returns raw key once)
// @Tags apikeys
// @Accept json
// @Produce json
// @Param body body createReq true "name"
// @Success 201 {object} keyDTO
// @Router /auth/api-keys [post]
func (h *Handler) Create(c *gin.Context) {
	uid, _, ok := h.Auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	var req createReq
	_ = c.ShouldBindJSON(&req)
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "default"
	}
	raw, prefix, err := newKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create key"})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tier := "free"
	var plan string
	_ = h.DB.QueryRow(`SELECT plan FROM users WHERE id = ?`, uid).Scan(&plan)
	if plan == "member" {
		tier = "paid"
	}
	res, err := h.DB.Exec(`
INSERT INTO api_keys (user_id, name, prefix, key_hash, rate_tier, created_at, revoked_at, last_used_at)
VALUES (?, ?, ?, ?, ?, ?, NULL, NULL)`, uid, name, prefix, bearer.Hash(raw), tier, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create key"})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(http.StatusCreated, keyDTO{
		ID: id, Name: name, Prefix: prefix, RateTier: tier, CreatedAt: now, Key: raw,
	})
}

// Revoke godoc
// @Summary Revoke an API key
// @Tags apikeys
// @Produce json
// @Param id path int true "key id"
// @Success 200 {object} map[string]bool
// @Router /auth/api-keys/{id}/revoke [post]
func (h *Handler) Revoke(c *gin.Context) {
	uid, _, ok := h.Auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	id := c.Param("id")
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := h.DB.Exec(`
UPDATE api_keys SET revoked_at = ?
WHERE id = ? AND user_id = ? AND (revoked_at IS NULL OR revoked_at = '')`, now, id, uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not revoke"})
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "key not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func newKey() (raw, prefix string, err error) {
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = "ah_" + base64.RawURLEncoding.EncodeToString(b)
	prefix = raw[:10]
	return raw, prefix, nil
}
