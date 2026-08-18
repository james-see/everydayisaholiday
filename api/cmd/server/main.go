package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/james-see/everydayisaholiday/api/internal/auth"
	"github.com/james-see/everydayisaholiday/api/internal/config"
	"github.com/james-see/everydayisaholiday/api/internal/db"
	"github.com/james-see/everydayisaholiday/api/internal/digest"
	"github.com/james-see/everydayisaholiday/api/internal/mail"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/james-see/everydayisaholiday/api/docs"
)

// @title A Day Is a Holiday API
// @version 1.0
// @description Auth and member API for adayisaholiday.com
// @BasePath /
// @cookie.name ah_session
func main() {
	loadDotEnv(".env")
	// Also load VPS env path when running under systemd from /home/jc/adayisaholidaycom
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
	}

	r.GET("/unsubscribe", digestH.Unsubscribe)
	r.POST("/unsubscribe", digestH.Unsubscribe)
	r.POST("/internal/digest/run", digestH.RunNow)

	log.Printf("listening on %s", cfg.ListenAddr)
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
			c.Header("Access-Control-Allow-Headers", "Content-Type, X-Digest-Token")
			c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,OPTIONS")
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
