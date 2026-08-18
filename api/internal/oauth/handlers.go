package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/james-see/everydayisaholiday/api/internal/auth"
	"github.com/james-see/everydayisaholiday/api/internal/bearer"
	"github.com/james-see/everydayisaholiday/api/internal/config"
)

const (
	defaultScope   = "holidays:read"
	codeTTL        = 10 * time.Minute
	accessTokenTTL = time.Hour
)

type Handler struct {
	DB     *sql.DB
	Cfg    config.Config
	Auth   *auth.Handler
	Issuer string // e.g. https://adayisaholiday.com
}

func (h *Handler) issuer() string {
	if h.Issuer != "" {
		return strings.TrimRight(h.Issuer, "/")
	}
	return strings.TrimRight(h.Cfg.PublicBaseURL, "/")
}

func (h *Handler) mcpResource() string {
	return h.issuer() + "/mcp"
}

// ASMetadata serves /.well-known/oauth-authorization-server
func (h *Handler) ASMetadata(c *gin.Context) {
	iss := h.issuer()
	c.JSON(http.StatusOK, gin.H{
		"issuer":                                iss,
		"authorization_endpoint":                iss + "/oauth/authorize",
		"token_endpoint":                        iss + "/oauth/token",
		"registration_endpoint":                 iss + "/oauth/register",
		"scopes_supported":                      []string{defaultScope},
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
	})
}

// PRMetadata serves /.well-known/oauth-protected-resource and /mcp variant
func (h *Handler) PRMetadata(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"resource":                 h.mcpResource(),
		"authorization_servers":    []string{h.issuer()},
		"scopes_supported":         []string{defaultScope},
		"bearer_methods_supported": []string{"header"},
	})
}

type registerReq struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

// Register implements RFC 7591 dynamic client registration.
func (h *Handler) Register(c *gin.Context) {
	var req registerReq
	if err := c.ShouldBindJSON(&req); err != nil || len(req.RedirectURIs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_client_metadata"})
		return
	}
	for _, u := range req.RedirectURIs {
		if !validRedirect(u) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_redirect_uri"})
			return
		}
	}
	method := req.TokenEndpointAuthMethod
	if method == "" {
		method = "none"
	}
	grants := req.GrantTypes
	if len(grants) == 0 {
		grants = []string{"authorization_code"}
	}
	clientID := "cli_" + randomURL(18)
	var secret, secretHash string
	if method != "none" {
		secret = randomURL(24)
		secretHash = bearer.Hash(secret)
	}
	uris, _ := json.Marshal(req.RedirectURIs)
	gtypes, _ := json.Marshal(grants)
	now := time.Now().UTC().Format(time.RFC3339)
	name := strings.TrimSpace(req.ClientName)
	if name == "" {
		name = "MCP client"
	}
	_, err := h.DB.Exec(`
INSERT INTO oauth_clients (client_id, client_secret_hash, client_name, redirect_uris, grant_types, token_endpoint_auth_method, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		clientID, nullIfEmpty(secretHash), name, string(uris), string(gtypes), method, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	out := gin.H{
		"client_id":                  clientID,
		"client_name":                name,
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                grants,
		"token_endpoint_auth_method": method,
		"client_id_issued_at":        time.Now().Unix(),
	}
	if secret != "" {
		out["client_secret"] = secret
	}
	c.JSON(http.StatusCreated, out)
}

// Authorize handles GET /oauth/authorize (authorization code + PKCE).
func (h *Handler) Authorize(c *gin.Context) {
	q := c.Request.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	challenge := q.Get("code_challenge")
	method := q.Get("code_challenge_method")
	state := q.Get("state")
	scope := q.Get("scope")
	resource := q.Get("resource")
	if method == "" {
		method = "S256"
	}
	if clientID == "" || redirectURI == "" || challenge == "" || method != "S256" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	if !h.clientAllowsRedirect(clientID, redirectURI) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "error_description": "redirect_uri mismatch"})
		return
	}
	if resource == "" {
		resource = h.mcpResource()
	}
	if scope == "" {
		scope = defaultScope
	}

	uid, email, ok := h.Auth.CurrentUser(c)
	if !ok {
		next := url.QueryEscape(c.Request.URL.RequestURI())
		c.Redirect(http.StatusFound, "/account.html?next="+next)
		return
	}

	if c.Query("approve") != "1" {
		c.Header("Content-Type", "text/html; charset=utf-8")
		approveURL := *c.Request.URL
		aq := approveURL.Query()
		aq.Set("approve", "1")
		approveURL.RawQuery = aq.Encode()
		fmt.Fprintf(c.Writer, `<!DOCTYPE html><html><body style="font-family:Georgia,serif;background:#1a1a2e;color:#e0e0e0;padding:2rem;max-width:480px;margin:auto">
<h1 style="color:#e94560">Authorize MCP access</h1>
<p>Signed in as <strong>%s</strong>.</p>
<p>Allow this app to read holiday data via MCP?</p>
<p><a href="%s" style="display:inline-block;padding:0.6rem 1rem;background:#e94560;color:#fff;text-decoration:none;border-radius:6px">Allow</a>
<a href="/" style="margin-left:1rem;color:#8a8a9a">Cancel</a></p>
</body></html>`, html.EscapeString(email), html.EscapeString(approveURL.String()))
		return
	}

	code := randomURL(32)
	now := time.Now().UTC()
	_, err := h.DB.Exec(`
INSERT INTO oauth_auth_codes (code_hash, client_id, user_id, redirect_uri, code_challenge, code_challenge_method, scope, resource, expires_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		bearer.Hash(code), clientID, uid, redirectURI, challenge, method, scope, resource,
		now.Add(codeTTL).Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	u, err := url.Parse(redirectURI)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	rq := u.Query()
	rq.Set("code", code)
	if state != "" {
		rq.Set("state", state)
	}
	rq.Set("iss", h.issuer())
	u.RawQuery = rq.Encode()
	c.Redirect(http.StatusFound, u.String())
}

// Token handles POST /oauth/token
func (h *Handler) Token(c *gin.Context) {
	_ = c.Request.ParseForm()
	grant := c.PostForm("grant_type")
	if grant != "authorization_code" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_grant_type"})
		return
	}
	code := c.PostForm("code")
	redirectURI := c.PostForm("redirect_uri")
	clientID := c.PostForm("client_id")
	verifier := c.PostForm("code_verifier")
	resource := c.PostForm("resource")
	if clientID == "" {
		user, pass, ok := c.Request.BasicAuth()
		if ok {
			clientID = user
			_ = pass
		}
	}
	if code == "" || redirectURI == "" || clientID == "" || verifier == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	if !h.authenticateClient(c, clientID) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_client"})
		return
	}

	var (
		userID     int64
		storedURI  string
		challenge  string
		method     string
		scope      string
		res        string
		expiresRaw string
	)
	err := h.DB.QueryRow(`
SELECT user_id, redirect_uri, code_challenge, code_challenge_method, scope, resource, expires_at
FROM oauth_auth_codes WHERE code_hash = ? AND client_id = ?`, bearer.Hash(code), clientID).
		Scan(&userID, &storedURI, &challenge, &method, &scope, &res, &expiresRaw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
		return
	}
	_, _ = h.DB.Exec(`DELETE FROM oauth_auth_codes WHERE code_hash = ?`, bearer.Hash(code))
	exp, _ := time.Parse(time.RFC3339, expiresRaw)
	if time.Now().UTC().After(exp) || storedURI != redirectURI {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
		return
	}
	if !verifyPKCE(verifier, challenge, method) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant", "error_description": "pkce failed"})
		return
	}
	if resource == "" {
		resource = res
	}
	if resource == "" {
		resource = h.mcpResource()
	}

	access := "oa_" + randomURL(32)
	now := time.Now().UTC()
	_, err = h.DB.Exec(`
INSERT INTO oauth_access_tokens (token_hash, user_id, client_id, scope, resource, expires_at, created_at, revoked_at)
VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`,
		bearer.Hash(access), userID, clientID, scope, resource,
		now.Add(accessTokenTTL).Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"access_token": access,
		"token_type":   "Bearer",
		"expires_in":   int(accessTokenTTL.Seconds()),
		"scope":        scope,
	})
}

func (h *Handler) clientAllowsRedirect(clientID, redirectURI string) bool {
	var urisJSON string
	err := h.DB.QueryRow(`SELECT redirect_uris FROM oauth_clients WHERE client_id = ?`, clientID).Scan(&urisJSON)
	if err != nil {
		return false
	}
	var uris []string
	_ = json.Unmarshal([]byte(urisJSON), &uris)
	for _, u := range uris {
		if u == redirectURI {
			return true
		}
	}
	return false
}

func (h *Handler) authenticateClient(c *gin.Context, clientID string) bool {
	var secretHash sql.NullString
	var method string
	err := h.DB.QueryRow(`
SELECT client_secret_hash, token_endpoint_auth_method FROM oauth_clients WHERE client_id = ?`, clientID).
		Scan(&secretHash, &method)
	if err != nil {
		return false
	}
	if method == "none" || !secretHash.Valid || secretHash.String == "" {
		return true
	}
	secret := c.PostForm("client_secret")
	if user, pass, ok := c.Request.BasicAuth(); ok && user == clientID {
		secret = pass
	}
	return secret != "" && bearer.Hash(secret) == secretHash.String
}

func validRedirect(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme == "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	if parsed.Scheme == "http" && (parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1") {
		return true
	}
	// custom schemes for native MCP clients
	if parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "" {
		return true
	}
	return false
}

func verifyPKCE(verifier, challenge, method string) bool {
	if method != "S256" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	calc := base64.RawURLEncoding.EncodeToString(sum[:])
	return calc == challenge
}

func randomURL(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}