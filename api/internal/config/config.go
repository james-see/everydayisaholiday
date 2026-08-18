package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr       string
	DatabasePath     string
	SessionSecret    string // reserved; sessions use random tokens
	CookieSecure     bool
	CookieDomain     string
	PublicBaseURL    string
	SessionTTL       time.Duration
	VerifyTTL        time.Duration
	ResetTTL         time.Duration
	ExposeVerifyToken  bool
	MailjetAPIKey      string
	MailjetSecretKey   string
	MailFrom           string
	HolidaysPath       string
	DigestHour         int
	DigestInterval     time.Duration
	DigestAdminToken   string
	StripeSecretKey    string
	StripeWebhookSecret string
	StripePriceMonthly string
	StripePriceYearly  string
}

func Load() Config {
	return Config{
		ListenAddr:         getenv("LISTEN_ADDR", "127.0.0.1:8083"),
		DatabasePath:       getenv("DATABASE_PATH", "./data/accounts.db"),
		SessionSecret:      getenv("SESSION_SECRET", ""),
		CookieSecure:       getenvBool("COOKIE_SECURE", true),
		CookieDomain:       getenv("COOKIE_DOMAIN", ""),
		PublicBaseURL:      strings.TrimRight(getenv("PUBLIC_BASE_URL", "https://adayisaholiday.com"), "/"),
		SessionTTL:         getenvDuration("SESSION_TTL", 30*24*time.Hour),
		VerifyTTL:          getenvDuration("VERIFY_TTL", 48*time.Hour),
		ResetTTL:           getenvDuration("RESET_TTL", 2*time.Hour),
		ExposeVerifyToken:  getenvBool("EXPOSE_VERIFY_TOKEN", false),
		MailjetAPIKey:      getenv("MAILJET_API_KEY", ""),
		MailjetSecretKey:   getenv("MAILJET_SECRET_KEY", ""),
		MailFrom:           getenv("MAIL_FROM", "A Day Is a Holiday <noreply@adayisaholiday.com>"),
		HolidaysPath:       getenv("HOLIDAYS_PATH", "/home/jc/adayisaholidaycom/site/holidays.json"),
		DigestHour:         getenvInt("DIGEST_HOUR", 8),
		DigestInterval:     getenvDuration("DIGEST_INTERVAL", 5*time.Minute),
		DigestAdminToken:    getenv("DIGEST_ADMIN_TOKEN", ""),
		StripeSecretKey:     getenv("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret: getenv("STRIPE_WEBHOOK_SECRET", ""),
		StripePriceMonthly:  getenv("STRIPE_PRICE_MONTHLY", ""),
		StripePriceYearly:   getenv("STRIPE_PRICE_YEARLY", ""),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func getenvDuration(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func getenvInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
