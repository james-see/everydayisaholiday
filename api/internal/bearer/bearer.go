package bearer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

var ErrUnauthorized = errors.New("unauthorized")
var ErrRateLimited = errors.New("rate limited")

type Principal struct {
	UserID   int64
	Email    string
	AuthVia  string // "api_key" | "oauth"
	KeyID    int64
	ClientID string
	Scopes   []string
	Tier     string // free | paid
}

type Validator struct {
	DB *sql.DB
}

func Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (v *Validator) FromRequest(r *http.Request) (*Principal, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return nil, ErrUnauthorized
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return nil, ErrUnauthorized
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	if token == "" {
		return nil, ErrUnauthorized
	}
	return v.ValidateToken(r.Context(), token)
}

func (v *Validator) ValidateToken(ctx context.Context, token string) (*Principal, error) {
	if strings.HasPrefix(token, "ah_") {
		return v.validateAPIKey(token)
	}
	return v.validateOAuth(token)
}

func (v *Validator) validateAPIKey(raw string) (*Principal, error) {
	var (
		id       int64
		userID   int64
		email    string
		tier     string
		revoked  sql.NullString
		verified sql.NullString
	)
	err := v.DB.QueryRow(`
SELECT k.id, k.user_id, u.email, k.rate_tier, k.revoked_at, u.email_verified_at
FROM api_keys k
JOIN users u ON u.id = k.user_id
WHERE k.key_hash = ?`, Hash(raw)).Scan(&id, &userID, &email, &tier, &revoked, &verified)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if revoked.Valid && revoked.String != "" {
		return nil, ErrUnauthorized
	}
	if !verified.Valid || verified.String == "" {
		return nil, ErrUnauthorized
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = v.DB.Exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, now, id)
	if tier == "" {
		tier = "free"
	}
	return &Principal{
		UserID:  userID,
		Email:   email,
		AuthVia: "api_key",
		KeyID:   id,
		Tier:    tier,
		Scopes:  []string{"holidays:read"},
	}, nil
}

func (v *Validator) validateOAuth(raw string) (*Principal, error) {
	now := time.Now().UTC()
	var (
		userID   int64
		email    string
		clientID string
		scope    string
		expires  string
		revoked  sql.NullString
		verified sql.NullString
	)
	err := v.DB.QueryRow(`
SELECT t.user_id, u.email, t.client_id, t.scope, t.expires_at, t.revoked_at, u.email_verified_at
FROM oauth_access_tokens t
JOIN users u ON u.id = t.user_id
WHERE t.token_hash = ?`, Hash(raw)).Scan(&userID, &email, &clientID, &scope, &expires, &revoked, &verified)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if revoked.Valid && revoked.String != "" {
		return nil, ErrUnauthorized
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil || now.After(exp) {
		return nil, ErrUnauthorized
	}
	if !verified.Valid || verified.String == "" {
		return nil, ErrUnauthorized
	}
	scopes := strings.Fields(scope)
	if len(scopes) == 0 {
		scopes = []string{"holidays:read"}
	}
	tier := "free"
	var plan string
	_ = v.DB.QueryRow(`SELECT plan FROM users WHERE id = ?`, userID).Scan(&plan)
	if plan == "member" {
		tier = "paid"
	}
	return &Principal{
		UserID:   userID,
		Email:    email,
		AuthVia:  "oauth",
		ClientID: clientID,
		Tier:     tier,
		Scopes:   scopes,
	}, nil
}

// CheckRateLimit returns ErrRateLimited if over limit for this principal.
func (v *Validator) CheckRateLimit(p *Principal) error {
	limit := 60
	if p.Tier == "paid" {
		limit = 600
	}
	window := time.Now().UTC().Unix() / 60
	key := p.AuthVia + ":"
	if p.AuthVia == "api_key" {
		key += "k" + itoa(p.KeyID)
	} else {
		key += "u" + itoa(p.UserID)
	}
	tx, err := v.DB.Begin()
	if err != nil {
		return nil
	}
	defer tx.Rollback()
	var count int
	err = tx.QueryRow(`SELECT count FROM rate_limits WHERE bucket_key = ? AND window_start = ?`, key, window).Scan(&count)
	if err == sql.ErrNoRows {
		_, err = tx.Exec(`INSERT INTO rate_limits (bucket_key, window_start, count) VALUES (?, ?, 1)`, key, window)
		if err != nil {
			return nil
		}
		_ = tx.Commit()
		return nil
	}
	if err != nil {
		return nil
	}
	if count >= limit {
		return ErrRateLimited
	}
	_, _ = tx.Exec(`UPDATE rate_limits SET count = count + 1 WHERE bucket_key = ? AND window_start = ?`, key, window)
	_ = tx.Commit()
	return nil
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
