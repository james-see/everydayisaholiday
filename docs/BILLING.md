# Membership (Stripe)

Optional paid membership for [adayisaholiday.com](https://adayisaholiday.com).

| Plan | Price | Entitlement |
|------|-------|-------------|
| Free | $0 | 60 API req/min |
| Member | **$5/mo** or **$48/yr** | 600 API req/min |

## Member UX

On [Account](https://adayisaholiday.com/account.html):

1. **Upgrade** → Stripe Checkout (branded “A Day Is a Holiday”)
2. **Manage billing / cancel** → Stripe Customer Portal (payment method, invoices, cancel)

Checkout uses per-session `branding_settings` so the hosted page shows this product’s name/colors even on a shared Stripe account.

## API

- `GET /billing/status`
- `POST /billing/checkout` `{ "interval": "month" | "year" }`
- `POST /billing/portal`
- `POST /billing/webhook` (Stripe signature)

Env (VPS `.env`): `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`, `STRIPE_PRICE_MONTHLY`, `STRIPE_PRICE_YEARLY`.

## Portal settings

In the Stripe Dashboard → [Customer portal](https://dashboard.stripe.com/settings/billing/portal), enable cancel, payment method update, and invoice history. Prefer business details that match A Day Is a Holiday where possible (portal uses account-level branding).

## Tax

If you charge US/EU customers at scale, enable [Stripe Tax](https://docs.stripe.com/billing/taxes/collect-taxes) and register where required before turning on `automatic_tax` — otherwise Stripe collects no tax while looking enabled.
