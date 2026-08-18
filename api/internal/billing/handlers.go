package billing

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/james-see/everydayisaholiday/api/internal/auth"
	"github.com/james-see/everydayisaholiday/api/internal/config"
	"github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/webhook"
)

type Handler struct {
	DB     *sql.DB
	Cfg    config.Config
	Auth   *auth.Handler
	Stripe *stripe.Client
}

type checkoutReq struct {
	Interval string `json:"interval"` // month | year
}

// Status godoc
// @Summary Membership status
// @Tags billing
// @Produce json
// @Success 200 {object} map[string]any
// @Router /billing/status [get]
func (h *Handler) Status(c *gin.Context) {
	uid, _, ok := h.Auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	st, err := h.loadStatus(uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load status"})
		return
	}
	c.JSON(http.StatusOK, st)
}

// Checkout godoc
// @Summary Start Stripe Checkout for membership
// @Tags billing
// @Accept json
// @Produce json
// @Param body body checkoutReq true "billing interval"
// @Success 200 {object} map[string]string
// @Router /billing/checkout [post]
func (h *Handler) Checkout(c *gin.Context) {
	if h.Stripe == nil || h.Cfg.StripePriceMonthly == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "billing not configured"})
		return
	}
	uid, email, ok := h.Auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	var verified sql.NullString
	_ = h.DB.QueryRow(`SELECT email_verified_at FROM users WHERE id = ?`, uid).Scan(&verified)
	if !verified.Valid || verified.String == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "verify your email before upgrading"})
		return
	}

	var req checkoutReq
	_ = c.ShouldBindJSON(&req)
	priceID := h.Cfg.StripePriceMonthly
	interval := "month"
	if strings.EqualFold(strings.TrimSpace(req.Interval), "year") {
		if h.Cfg.StripePriceYearly == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "yearly plan not configured"})
			return
		}
		priceID = h.Cfg.StripePriceYearly
		interval = "year"
	}

	customerID, err := h.ensureCustomer(c.Request.Context(), uid, email)
	if err != nil {
		log.Printf("billing: ensure customer: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start checkout"})
		return
	}

	base := strings.TrimRight(h.Cfg.PublicBaseURL, "/")
	integ := "adayisaholiday_member_" + randSuffix(8)
	bg := "#1a1a2e"
	btn := "#e94560"
	name := "A Day Is a Holiday"
	font := "lora"
	border := "rounded"
	params := &stripe.CheckoutSessionCreateParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		Customer:          stripe.String(customerID),
		ClientReferenceID: stripe.String(fmt.Sprintf("%d", uid)),
		SuccessURL:        stripe.String(base + "/account.html?billing=success"),
		CancelURL:         stripe.String(base + "/account.html?billing=canceled"),
		AllowPromotionCodes: stripe.Bool(true),
		BillingAddressCollection: stripe.String("auto"),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{
			{Price: stripe.String(priceID), Quantity: stripe.Int64(1)},
		},
		SubscriptionData: &stripe.CheckoutSessionCreateSubscriptionDataParams{
			Metadata: map[string]string{
				"user_id":  fmt.Sprintf("%d", uid),
				"project":  "adayisaholiday",
				"interval": interval,
			},
		},
		Metadata: map[string]string{
			"user_id":  fmt.Sprintf("%d", uid),
			"project":  "adayisaholiday",
			"interval": interval,
		},
		IntegrationIdentifier: stripe.String(integ),
		BrandingSettings: &stripe.CheckoutSessionCreateBrandingSettingsParams{
			BackgroundColor: stripe.String(bg),
			ButtonColor:     stripe.String(btn),
			DisplayName:     stripe.String(name),
			FontFamily:      stripe.String(font),
			BorderStyle:     stripe.String(border),
		},
		CustomText: &stripe.CheckoutSessionCreateCustomTextParams{
			Submit: &stripe.CheckoutSessionCreateCustomTextSubmitParams{
				Message: stripe.String("You're supporting A Day Is a Holiday — higher API limits and member perks."),
			},
		},
	}

	sess, err := h.Stripe.V1CheckoutSessions.Create(c.Request.Context(), params)
	if err != nil {
		log.Printf("billing: checkout: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start checkout", "detail": stripeMessage(err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": sess.URL})
}

// Portal godoc
// @Summary Open Stripe Customer Portal
// @Tags billing
// @Produce json
// @Success 200 {object} map[string]string
// @Router /billing/portal [post]
func (h *Handler) Portal(c *gin.Context) {
	if h.Stripe == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "billing not configured"})
		return
	}
	uid, email, ok := h.Auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	customerID, err := h.ensureCustomer(c.Request.Context(), uid, email)
	if err != nil {
		log.Printf("billing: portal customer: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not open portal"})
		return
	}
	base := strings.TrimRight(h.Cfg.PublicBaseURL, "/")
	sess, err := h.Stripe.V1BillingPortalSessions.Create(c.Request.Context(), &stripe.BillingPortalSessionCreateParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(base + "/account.html?billing=portal"),
	})
	if err != nil {
		log.Printf("billing: portal: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not open portal", "detail": stripeMessage(err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": sess.URL})
}

// Webhook godoc
// @Summary Stripe webhooks
// @Tags billing
// @Router /billing/webhook [post]
func (h *Handler) Webhook(c *gin.Context) {
	if h.Cfg.StripeWebhookSecret == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "webhook not configured"})
		return
	}
	const maxBody = int64(65536)
	payload, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBody))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	sig := c.GetHeader("Stripe-Signature")
	event, err := webhook.ConstructEvent(payload, sig, h.Cfg.StripeWebhookSecret)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid signature"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := h.DB.Exec(`INSERT OR IGNORE INTO stripe_events (event_id, event_type, processed_at) VALUES (?, ?, ?)`,
		event.ID, string(event.Type), now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db"})
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": true, "duplicate": true})
		return
	}

	if err := h.handleEvent(c.Request.Context(), event); err != nil {
		log.Printf("billing: webhook %s: %v", event.ID, err)
		_, _ = h.DB.Exec(`DELETE FROM stripe_events WHERE event_id = ?`, event.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) handleEvent(ctx context.Context, event stripe.Event) error {
	switch event.Type {
	case "checkout.session.completed":
		var sess stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
			return err
		}
		return h.applyCheckoutSession(ctx, &sess)
	case "customer.subscription.updated", "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return err
		}
		return h.applySubscription(ctx, &sub)
	case "invoice.paid", "invoice.payment_failed":
		var inv stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
			return err
		}
		return h.applyInvoice(ctx, &inv, string(event.Type))
	default:
		return nil
	}
}

func (h *Handler) applyCheckoutSession(ctx context.Context, sess *stripe.CheckoutSession) error {
	if sess.Mode != stripe.CheckoutSessionModeSubscription {
		return nil
	}
	uid, err := h.resolveUserID(sess.ClientReferenceID, sess.Metadata, customerID(sess.Customer))
	if err != nil {
		return err
	}
	if cid := customerID(sess.Customer); cid != "" {
		_, _ = h.DB.Exec(`UPDATE users SET stripe_customer_id = ?, updated_at = ? WHERE id = ?`,
			cid, time.Now().UTC().Format(time.RFC3339), uid)
	}
	if sess.Subscription == nil || sess.Subscription.ID == "" {
		return h.setPlan(uid, "member", "active", "", "")
	}
	sub, err := h.Stripe.V1Subscriptions.Retrieve(ctx, sess.Subscription.ID, nil)
	if err != nil {
		return err
	}
	return h.applySubscription(ctx, sub)
}

func (h *Handler) applySubscription(_ context.Context, sub *stripe.Subscription) error {
	uid, err := h.resolveUserID("", sub.Metadata, customerID(sub.Customer))
	if err != nil {
		return err
	}
	status := string(sub.Status)
	plan := "free"
	if status == "active" || status == "trialing" || status == "past_due" {
		plan = "member"
	}
	if status == "canceled" || status == "unpaid" || status == "incomplete_expired" {
		plan = "free"
	}
	periodEnd := ""
	if sub.Items != nil && len(sub.Items.Data) > 0 && sub.Items.Data[0].CurrentPeriodEnd > 0 {
		periodEnd = time.Unix(sub.Items.Data[0].CurrentPeriodEnd, 0).UTC().Format(time.RFC3339)
	}
	return h.setPlan(uid, plan, status, sub.ID, periodEnd)
}

func (h *Handler) applyInvoice(ctx context.Context, inv *stripe.Invoice, eventType string) error {
	subID := ""
	if inv.Parent != nil && inv.Parent.SubscriptionDetails != nil && inv.Parent.SubscriptionDetails.Subscription != nil {
		subID = inv.Parent.SubscriptionDetails.Subscription.ID
	}
	if subID == "" {
		return nil
	}
	sub, err := h.Stripe.V1Subscriptions.Retrieve(ctx, subID, nil)
	if err != nil {
		return err
	}
	if eventType == "invoice.payment_failed" && sub.Status == stripe.SubscriptionStatusActive {
		// keep member but surface past_due if Stripe flipped; re-apply retrieved status
	}
	return h.applySubscription(ctx, sub)
}

func (h *Handler) setPlan(uid int64, plan, status, subID, periodEnd string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := h.DB.Exec(`
UPDATE users
SET plan = ?, subscription_status = ?, stripe_subscription_id = ?, current_period_end = ?, updated_at = ?
WHERE id = ?`, plan, status, nullEmpty(subID), nullEmpty(periodEnd), now, uid)
	if err != nil {
		return err
	}
	tier := "free"
	if plan == "member" {
		tier = "paid"
	}
	_, _ = h.DB.Exec(`
UPDATE api_keys SET rate_tier = ?
WHERE user_id = ? AND (revoked_at IS NULL OR revoked_at = '')`, tier, uid)
	return nil
}

func (h *Handler) ensureCustomer(ctx context.Context, uid int64, email string) (string, error) {
	var existing sql.NullString
	err := h.DB.QueryRow(`SELECT stripe_customer_id FROM users WHERE id = ?`, uid).Scan(&existing)
	if err != nil {
		return "", err
	}
	if existing.Valid && existing.String != "" {
		return existing.String, nil
	}
	cust, err := h.Stripe.V1Customers.Create(ctx, &stripe.CustomerCreateParams{
		Email: stripe.String(email),
		Metadata: map[string]string{
			"user_id": fmt.Sprintf("%d", uid),
			"project": "adayisaholiday",
		},
	})
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = h.DB.Exec(`UPDATE users SET stripe_customer_id = ?, updated_at = ? WHERE id = ?`, cust.ID, now, uid)
	if err != nil {
		return "", err
	}
	return cust.ID, nil
}

func (h *Handler) resolveUserID(clientRef string, meta map[string]string, customer string) (int64, error) {
	if clientRef != "" {
		var id int64
		if _, err := fmt.Sscanf(clientRef, "%d", &id); err == nil && id > 0 {
			return id, nil
		}
	}
	if meta != nil {
		if s := meta["user_id"]; s != "" {
			var id int64
			if _, err := fmt.Sscanf(s, "%d", &id); err == nil && id > 0 {
				return id, nil
			}
		}
	}
	if customer != "" {
		var id int64
		err := h.DB.QueryRow(`SELECT id FROM users WHERE stripe_customer_id = ?`, customer).Scan(&id)
		if err == nil {
			return id, nil
		}
	}
	return 0, fmt.Errorf("could not resolve user")
}

func (h *Handler) loadStatus(uid int64) (gin.H, error) {
	var (
		plan, status string
		periodEnd    sql.NullString
		customer     sql.NullString
		subID        sql.NullString
	)
	err := h.DB.QueryRow(`
SELECT plan, subscription_status, current_period_end, stripe_customer_id, stripe_subscription_id
FROM users WHERE id = ?`, uid).Scan(&plan, &status, &periodEnd, &customer, &subID)
	if err != nil {
		return nil, err
	}
	if plan == "" {
		plan = "free"
	}
	out := gin.H{
		"plan":                 plan,
		"subscription_status":  status,
		"current_period_end":   nil,
		"has_stripe_customer":  customer.Valid && customer.String != "",
		"stripe_subscription_id": nil,
		"prices": gin.H{
			"monthly_cents": 500,
			"yearly_cents":  4800,
			"currency":      "usd",
		},
	}
	if periodEnd.Valid && periodEnd.String != "" {
		out["current_period_end"] = periodEnd.String
	}
	if subID.Valid && subID.String != "" {
		out["stripe_subscription_id"] = subID.String
	}
	return out, nil
}

// stripeMessage returns the customer-safe Stripe error message, if any.
func stripeMessage(err error) string {
	var serr *stripe.Error
	if errors.As(err, &serr) && serr.Msg != "" {
		return serr.Msg
	}
	return ""
}

func customerID(c *stripe.Customer) string {
	if c == nil {
		return ""
	}
	return c.ID
}

func nullEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func randSuffix(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "xxxxxxxx"
	}
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b)
}

// UserPlan returns free|member for a user id.
func UserPlan(db *sql.DB, uid int64) string {
	var plan string
	err := db.QueryRow(`SELECT plan FROM users WHERE id = ?`, uid).Scan(&plan)
	if err != nil || plan == "" {
		return "free"
	}
	return plan
}
