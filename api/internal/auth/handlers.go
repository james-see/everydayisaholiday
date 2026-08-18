package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/james-see/everydayisaholiday/api/internal/config"
	mailer "github.com/james-see/everydayisaholiday/api/internal/mail"
	"golang.org/x/crypto/bcrypt"
)

const (
	cookieName   = "ah_session"
	bcryptCost   = 12
	minPassword  = 10
	maxPassword  = 128
	maxEmailLen  = 254
)

var (
	errInvalidCreds = errors.New("invalid email or password")
	errUnverified   = errors.New("email not verified")
	errConflict     = errors.New("email already registered")
)

type Handler struct {
	DB     *sql.DB
	Cfg    config.Config
	Mailer mailer.Sender
}

type signupReq struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type loginReq struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type forgotReq struct {
	Email string `json:"email" binding:"required"`
}

type resetReq struct {
	Token    string `json:"token" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type userDTO struct {
	ID              int64  `json:"id"`
	Email           string `json:"email"`
	EmailVerifiedAt *string `json:"email_verified_at"`
	CreatedAt       string `json:"created_at"`
}

// Signup godoc
// @Summary Register a new account
// @Tags auth
// @Accept json
// @Produce json
// @Param body body signupReq true "credentials"
// @Success 201 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /auth/signup [post]
func (h *Handler) Signup(c *gin.Context) {
	var req signupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email"})
		return
	}
	if err := validatePassword(req.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create account"})
		return
	}
	rawToken, tokenHash, err := newToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create account"})
		return
	}
	now := time.Now().UTC()
	expires := now.Add(h.Cfg.VerifyTTL).Format(time.RFC3339)
	created := now.Format(time.RFC3339)

	res, err := h.DB.Exec(`
INSERT INTO users (
  email, password_hash, email_verified_at,
  verify_token_hash, verify_token_expires_at,
  reset_token_hash, reset_token_expires_at,
  created_at, updated_at
) VALUES (?, ?, NULL, ?, ?, NULL, NULL, ?, ?)`,
		email, string(hash), tokenHash, expires, created, created,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			c.JSON(http.StatusConflict, gin.H{"error": errConflict.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create account"})
		return
	}
	uid, _ := res.LastInsertId()

	verifyURL := fmt.Sprintf("%s/auth/verify?token=%s", h.Cfg.PublicBaseURL, rawToken)
	if err := h.Mailer.Send(email, "Verify your A Day Is a Holiday account",
		"Thanks for signing up.\n\nVerify your email:\n"+verifyURL+"\n\nThis link expires in "+h.Cfg.VerifyTTL.String()+".\n"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "account created but verification email failed to send"})
		return
	}

	out := gin.H{
		"id":      uid,
		"email":   email,
		"message": "check your email to verify your account",
	}
	if h.Cfg.ExposeVerifyToken {
		out["verify_token"] = rawToken
		out["verify_url"] = verifyURL
	}
	c.JSON(http.StatusCreated, out)
}

// ResendVerification godoc
// @Summary Resend email verification link
// @Tags auth
// @Accept json
// @Produce json
// @Param body body forgotReq true "email"
// @Success 200 {object} map[string]string
// @Router /auth/resend-verification [post]
func (h *Handler) ResendVerification(c *gin.Context) {
	var req forgotReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	email, err := normalizeEmail(req.Email)
	out := gin.H{"message": "if that account needs verification, a new link was sent"}
	if err != nil {
		c.JSON(http.StatusOK, out)
		return
	}
	var (
		id         int64
		verifiedAt sql.NullString
	)
	err = h.DB.QueryRow(`
SELECT id, email_verified_at FROM users WHERE email = ?`, email).Scan(&id, &verifiedAt)
	if err != nil {
		c.JSON(http.StatusOK, out)
		return
	}
	if verifiedAt.Valid && verifiedAt.String != "" {
		c.JSON(http.StatusOK, gin.H{"message": "email already verified"})
		return
	}
	rawToken, tokenHash, err := newToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not resend"})
		return
	}
	now := time.Now().UTC()
	expires := now.Add(h.Cfg.VerifyTTL).Format(time.RFC3339)
	_, err = h.DB.Exec(`
UPDATE users
SET verify_token_hash = ?, verify_token_expires_at = ?, updated_at = ?
WHERE id = ?`, tokenHash, expires, now.Format(time.RFC3339), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not resend"})
		return
	}
	verifyURL := fmt.Sprintf("%s/auth/verify?token=%s", h.Cfg.PublicBaseURL, rawToken)
	if err := h.Mailer.Send(email, "Verify your A Day Is a Holiday account",
		"Verify your email:\n"+verifyURL+"\n\nThis link expires in "+h.Cfg.VerifyTTL.String()+".\n"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not send verification email"})
		return
	}
	c.JSON(http.StatusOK, out)
}

// Login godoc
// @Summary Log in
// @Tags auth
// @Accept json
// @Produce json
// @Param body body loginReq true "credentials"
// @Success 200 {object} userDTO
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Router /auth/login [post]
func (h *Handler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": errInvalidCreds.Error()})
		return
	}

	var (
		id           int64
		passwordHash string
		verifiedAt   sql.NullString
		createdAt    string
	)
	err = h.DB.QueryRow(`
SELECT id, password_hash, email_verified_at, created_at
FROM users WHERE email = ?`, email).Scan(&id, &passwordHash, &verifiedAt, &createdAt)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$12$invalidhashinvalidhashinvalidho"), []byte(req.Password))
		c.JSON(http.StatusUnauthorized, gin.H{"error": errInvalidCreds.Error()})
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": errInvalidCreds.Error()})
		return
	}
	if !verifiedAt.Valid || verifiedAt.String == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": errUnverified.Error()})
		return
	}
	if err := h.createSession(c, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create session"})
		return
	}
	c.JSON(http.StatusOK, userDTO{
		ID:              id,
		Email:           email,
		EmailVerifiedAt: &verifiedAt.String,
		CreatedAt:       createdAt,
	})
}

// Logout godoc
// @Summary Log out
// @Tags auth
// @Produce json
// @Success 200 {object} map[string]string
// @Router /auth/logout [post]
func (h *Handler) Logout(c *gin.Context) {
	raw, err := c.Cookie(cookieName)
	if err == nil && raw != "" {
		_, _ = h.DB.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashToken(raw))
	}
	h.clearCookie(c)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Me godoc
// @Summary Current user
// @Tags auth
// @Produce json
// @Success 200 {object} userDTO
// @Failure 401 {object} map[string]string
// @Router /auth/me [get]
func (h *Handler) Me(c *gin.Context) {
	u, ok := h.userFromCookie(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	c.JSON(http.StatusOK, u)
}

// Verify godoc
// @Summary Verify email via token
// @Tags auth
// @Produce json
// @Param token query string true "verification token"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /auth/verify [get]
func (h *Handler) Verify(c *gin.Context) {
	raw := strings.TrimSpace(c.Query("token"))
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing token"})
		return
	}
	now := time.Now().UTC()
	var (
		id         int64
		email      string
		expiresRaw sql.NullString
	)
	err := h.DB.QueryRow(`
SELECT id, email, verify_token_expires_at
FROM users
WHERE verify_token_hash = ?`, hashToken(raw)).Scan(&id, &email, &expiresRaw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired token"})
		return
	}
	if !expiresRaw.Valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired token"})
		return
	}
	exp, err := time.Parse(time.RFC3339, expiresRaw.String)
	if err != nil || now.After(exp) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired token"})
		return
	}
	verified := now.Format(time.RFC3339)
	_, err = h.DB.Exec(`
UPDATE users
SET email_verified_at = ?,
    verify_token_hash = NULL,
    verify_token_expires_at = NULL,
    updated_at = ?
WHERE id = ?`, verified, verified, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not verify"})
		return
	}
	if err := h.createSession(c, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "verified but session failed"})
		return
	}

	accept := c.GetHeader("Accept")
	if strings.Contains(accept, "text/html") || c.Query("redirect") == "1" || c.Query("format") == "html" {
		c.Redirect(http.StatusFound, "/account.html?verified=1")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":    true,
		"email": email,
		"id":    id,
	})
}

// ForgotPassword godoc
// @Summary Request password reset
// @Tags auth
// @Accept json
// @Produce json
// @Param body body forgotReq true "email"
// @Success 200 {object} map[string]any
// @Router /auth/forgot [post]
func (h *Handler) ForgotPassword(c *gin.Context) {
	var req forgotReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "if that account exists, a reset link was sent"})
		return
	}
	var id int64
	err = h.DB.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id)
	out := gin.H{"message": "if that account exists, a reset link was sent"}
	if err != nil {
		c.JSON(http.StatusOK, out)
		return
	}
	rawToken, tokenHash, err := newToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start reset"})
		return
	}
	now := time.Now().UTC()
	expires := now.Add(h.Cfg.ResetTTL).Format(time.RFC3339)
	_, err = h.DB.Exec(`
UPDATE users
SET reset_token_hash = ?, reset_token_expires_at = ?, updated_at = ?
WHERE id = ?`, tokenHash, expires, now.Format(time.RFC3339), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start reset"})
		return
	}
	resetURL := fmt.Sprintf("%s/account.html?reset=%s", h.Cfg.PublicBaseURL, rawToken)
	_ = h.Mailer.Send(email, "Reset your A Day Is a Holiday password",
		"Reset your password:\n"+resetURL+"\n\nThis link expires in "+h.Cfg.ResetTTL.String()+".\n")
	if h.Cfg.ExposeVerifyToken {
		out["reset_token"] = rawToken
		out["reset_url"] = resetURL
	}
	c.JSON(http.StatusOK, out)
}

// ResetPassword godoc
// @Summary Reset password with token
// @Tags auth
// @Accept json
// @Produce json
// @Param body body resetReq true "token and new password"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Router /auth/reset [post]
func (h *Handler) ResetPassword(c *gin.Context) {
	var req resetReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if err := validatePassword(req.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var (
		id         int64
		expiresRaw sql.NullString
	)
	err := h.DB.QueryRow(`
SELECT id, reset_token_expires_at
FROM users WHERE reset_token_hash = ?`, hashToken(req.Token)).Scan(&id, &expiresRaw)
	if err != nil || !expiresRaw.Valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired token"})
		return
	}
	exp, err := time.Parse(time.RFC3339, expiresRaw.String)
	if err != nil || time.Now().UTC().After(exp) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired token"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not reset password"})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = h.DB.Exec(`
UPDATE users
SET password_hash = ?,
    reset_token_hash = NULL,
    reset_token_expires_at = NULL,
    updated_at = ?
WHERE id = ?`, string(hash), now, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not reset password"})
		return
	}
	_, _ = h.DB.Exec(`DELETE FROM sessions WHERE user_id = ?`, id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) createSession(c *gin.Context, userID int64) error {
	raw, tokenHash, err := newToken()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	expires := now.Add(h.Cfg.SessionTTL)
	_, err = h.DB.Exec(`
INSERT INTO sessions (user_id, token_hash, expires_at, created_at)
VALUES (?, ?, ?, ?)`,
		userID, tokenHash, expires.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		return err
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     cookieName,
		Value:    raw,
		Path:     "/",
		Domain:   h.Cfg.CookieDomain,
		Expires:  expires,
		MaxAge:   int(h.Cfg.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.Cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (h *Handler) clearCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		Domain:   h.Cfg.CookieDomain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.Cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) userFromCookie(c *gin.Context) (userDTO, bool) {
	raw, err := c.Cookie(cookieName)
	if err != nil || raw == "" {
		return userDTO{}, false
	}
	now := time.Now().UTC()
	var u userDTO
	var verified sql.NullString
	err = h.DB.QueryRow(`
SELECT u.id, u.email, u.email_verified_at, u.created_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = ? AND s.expires_at > ?`,
		hashToken(raw), now.Format(time.RFC3339),
	).Scan(&u.ID, &u.Email, &verified, &u.CreatedAt)
	if err != nil {
		h.clearCookie(c)
		return userDTO{}, false
	}
	if verified.Valid {
		u.EmailVerifiedAt = &verified.String
	}
	return u, true
}

// CurrentUser returns the authenticated user for the request, if any.
func (h *Handler) CurrentUser(c *gin.Context) (id int64, email string, ok bool) {
	u, ok := h.userFromCookie(c)
	if !ok {
		return 0, "", false
	}
	return u.ID, u.Email, true
}

// NewToken returns a raw URL-safe token and its SHA-256 hex hash.
func NewToken() (raw string, hash string, err error) {
	return newToken()
}

// HashToken hashes a raw token the same way sessions/unsubscribe tokens are stored.
func HashToken(raw string) string {
	return hashToken(raw)
}

func normalizeEmail(email string) (string, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || utf8.RuneCountInString(email) > maxEmailLen {
		return "", errors.New("invalid email")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", errors.New("invalid email")
	}
	return email, nil
}

func validatePassword(pw string) error {
	n := utf8.RuneCountInString(pw)
	if n < minPassword {
		return fmt.Errorf("password must be at least %d characters", minPassword)
	}
	if n > maxPassword {
		return fmt.Errorf("password too long")
	}
	return nil
}

func newToken() (raw string, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
