package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/james-see/everydayisaholiday/api/internal/apikey"
	"github.com/james-see/everydayisaholiday/api/internal/auth"
	"github.com/james-see/everydayisaholiday/api/internal/bearer"
	"github.com/james-see/everydayisaholiday/api/internal/billing"
	"github.com/james-see/everydayisaholiday/api/internal/config"
	"github.com/james-see/everydayisaholiday/api/internal/db"
	"github.com/james-see/everydayisaholiday/api/internal/digest"
	"github.com/james-see/everydayisaholiday/api/internal/mail"
	"github.com/james-see/everydayisaholiday/api/internal/mcpserver"
	"github.com/james-see/everydayisaholiday/api/internal/oauth"
	"github.com/james-see/everydayisaholiday/api/internal/v1api"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/stripe/stripe-go/v86"

	_ "github.com/james-see/everydayisaholiday/api/docs"
)

// @title A Day Is a Holiday API
// @version 1.0
// @description Auth, digest, member API, OAuth, and MCP for adayisaholiday.com
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	loadDotEnv(".env")
	loadDotEnv("/home/jc/adayisaholidaycom/.env")
	cfg := config.Load()
	if cfg.SessionSecret == "" {
		log.Println("warning: SESSION_SECRET is empty; set it in production")
	}

	sqlDB, err := db.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer sqlDB.Close()

	mailer := mail.New(cfg.MailjetAPIKey, cfg.MailjetSecretKey, cfg.MailFrom)
	if cfg.MailjetAPIKey != "" && cfg.MailjetSecretKey != "" {
		log.Println("mail: mailjet enabled")
	} else {
		log.Println("mail: log-only (set MAILJET_API_KEY + MAILJET_SECRET_KEY)")
	}

	holidays, err := digest.Load(cfg.HolidaysPath)
	if err != nil {
		log.Fatalf("holidays: %v (set HOLIDAYS_PATH)", err)
	}
	log.Printf("holidays: loaded from %s (%d categories)", cfg.HolidaysPath, len(holidays.Categories()))

	authH := &auth.Handler{DB: sqlDB, Cfg: cfg, Mailer: mailer}
	runner := &digest.Runner{DB: sqlDB, Cfg: cfg, Mailer: mailer, Holidays: holidays}
	digestH := &digest.Handler{
		DB: sqlDB, Cfg: cfg, Auth: authH, Mailer: mailer, Holidays: holidays, Runner: runner,
	}
	runner.StartLoop()

	validator := &bearer.Validator{DB: sqlDB}
	keysH := &apikey.Handler{DB: sqlDB, Auth: authH}
	oauthH := &oauth.Handler{DB: sqlDB, Cfg: cfg, Auth: authH, Issuer: cfg.PublicBaseURL}
	v1H := &v1api.Handler{Holidays: holidays, Validator: validator}
	mcpSrv := mcpserver.New(holidays, validator, cfg.PublicBaseURL)

	var stripeClient *stripe.Client
	if cfg.StripeSecretKey != "" {
		stripeClient = stripe.NewClient(cfg.StripeSecretKey)
		log.Println("billing: stripe enabled")
	} else {
		log.Println("billing: stripe disabled (set STRIPE_SECRET_KEY)")
	}
	billingH := &billing.Handler{DB: sqlDB, Cfg: cfg, Auth: authH, Stripe: stripeClient}

	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger(), corsMiddleware())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	authGroup := r.Group("/auth")
	{
		authGroup.POST("/signup", authH.Signup)
		authGroup.POST("/login", authH.Login)
		authGroup.POST("/logout", authH.Logout)
		authGroup.GET("/me", authH.Me)
		authGroup.GET("/verify", authH.Verify)
		authGroup.POST("/resend-verification", authH.ResendVerification)
		authGroup.POST("/forgot", authH.ForgotPassword)
		authGroup.POST("/reset", authH.ResetPassword)
		authGroup.GET("/email-prefs", digestH.GetPrefs)
		authGroup.PUT("/email-prefs", digestH.PutPrefs)
		authGroup.GET("/email-prefs/categories", digestH.Categories)
		authGroup.POST("/email-prefs/preview", digestH.Preview)
		authGroup.GET("/api-keys", keysH.List)
		authGroup.POST("/api-keys", keysH.Create)
		authGroup.POST("/api-keys/:id/revoke", keysH.Revoke)
	}

	r.GET("/unsubscribe", digestH.Unsubscribe)
	r.POST("/unsubscribe", digestH.Unsubscribe)
	r.POST("/internal/digest/run", digestH.RunNow)

	r.GET("/.well-known/oauth-authorization-server", oauthH.ASMetadata)
	r.GET("/.well-known/oauth-protected-resource", oauthH.PRMetadata)
	r.GET("/.well-known/oauth-protected-resource/mcp", oauthH.PRMetadata)
	r.POST("/oauth/register", oauthH.Register)
	r.GET("/oauth/authorize", oauthH.Authorize)
	r.POST("/oauth/token", oauthH.Token)

	v1 := r.Group("/v1", v1H.Middleware())
	{
		v1.GET("/today", v1H.Today)
		v1.GET("/holidays", v1H.List)
		v1.GET("/holidays/:mmdd", v1H.ByDate)
	}

	r.Any("/mcp", gin.WrapH(mcpSrv.Handler()))
	r.Any("/mcp/*path", gin.WrapH(mcpSrv.Handler()))

	billingGroup := r.Group("/billing")
	{
		billingGroup.GET("/status", billingH.Status)
		billingGroup.POST("/checkout", billingH.Checkout)
		billingGroup.POST("/portal", billingH.Portal)
		billingGroup.POST("/webhook", billingH.Webhook)
	}

	log.Printf("listening on %s (mcp %s/mcp)", cfg.ListenAddr, strings.TrimRight(cfg.PublicBaseURL, "/"))
	if err := r.Run(cfg.ListenAddr); err != nil {
		log.Fatal(err)
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		switch origin {
		case "https://adayisaholiday.com", "http://127.0.0.1:8766", "http://localhost:8766", "http://127.0.0.1:8083":
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Digest-Token, Mcp-Session-Id")
			c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
			c.Header("Access-Control-Expose-Headers", "Mcp-Session-Id")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func loadDotEnv(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 {
			if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
				v = v[1 : len(v)-1]
			}
		}
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}
