# Architecture ADR — client cache vs VPS backend

**Status:** Accepted  
**Date:** 2026-08-17  
**Issue:** [#2](https://github.com/james-see/everydayisaholiday/issues/2) (epic [#1](https://github.com/james-see/everydayisaholiday/issues/1))

## Context

The site is a static calendar on a VPS (Caddy → `/home/jc/adayisaholidaycom/site/`). Member accounts, daily email, API keys, and Stripe need a trusted backend. Browser SQLite is useful for offline holiday browsing, not for secrets or entitlements. Postgres is too heavy for this droplet.

## Decision

| Layer | Choice | Role |
|-------|--------|------|
| Browser holidays | Official **SQLite WASM** (`sqlite3.wasm`) + **OPFS**, IndexedDB/memory fallback | Holiday **cache + offline** only |
| VPS holidays / API reads | Embedded **DuckDB** file | Authoritative holiday dataset; member API queries |
| VPS accounts / billing | Embedded **SQLite** file | Users, sessions, prefs, API key hashes, Stripe entitlements |
| API | **Go gin**, **systemd** unit `adayisaholiday-api.service` | Auth, email, Stripe, `/v1/*` |
| Edge | Existing **Caddy** for `adayisaholiday.com` | Static root + reverse proxy to gin |
| Out of scope | **Postgres**, browser-stored credentials/secrets | — |

### Libraries

- **Client:** official SQLite Wasm build (assets under `docs/wasm/`); prefer OPFS via `installOpfsSAHPoolVfs` when available; fall back to in-memory SQLite, then plain JSON if WASM fails. OPFS requires cross-origin isolation — Caddy must send `Cross-Origin-Opener-Policy: same-origin` and `Cross-Origin-Embedder-Policy: require-corp` on the site.
- **Server:** Go DuckDB driver + SQLite driver; **no ORM** — prepared statements with explicit column lists.
- **Auth transport:** HTTP-only session cookies (CSRF as needed); API keys via `Authorization: Bearer` (hashes only in SQLite).

### Sync model

1. Canonical holiday source remains `docs/holidays.json` (and Checkiday refresh scripts).
2. On deploy / admin import: load JSON into DuckDB on the VPS.
3. Browser: pull `/holidays.json` (or a versioned sync endpoint) into SQLite WASM — idempotent, version-aware import.
4. Online UI may query the API; offline UI queries local WASM cache only.
5. Account/billing state is always server-authoritative; client may cache non-sensitive prefs display-only.

### VPS process layout

- **Binary / data (not web-root):** `/home/jc/adayisaholidaycom/`  
  - e.g. `bin/adayisaholiday-api`  
  - `data/holidays.duckdb`  
  - `data/accounts.db`  
  - env file for Stripe / email / session secrets (mode `600`)
- **Static site:** `/home/jc/adayisaholidaycom/site/` (rsync from repo `docs/`)
- **Listen:** `127.0.0.1:8083` only (ports `8080`–`8082` already taken on this host)
- **systemd:** `adayisaholiday-api.service` — `Restart=on-failure`, starts gin bound to `:8083`
- **Caddy:** keep `root` for the calendar; proxy API paths to localhost:

```caddy
adayisaholiday.com {
	root * /home/jc/adayisaholidaycom/site/
	encode gzip
	# Required for SQLite WASM OPFS persistence (cache/offline)
	header Cross-Origin-Opener-Policy same-origin
	header Cross-Origin-Embedder-Policy require-corp

	handle /v1/* {
		reverse_proxy 127.0.0.1:8083
	}
	handle /auth/* {
		reverse_proxy 127.0.0.1:8083
	}
	handle {
		file_server
	}
}
```

Exact path prefixes may grow (`/webhooks/stripe`, etc.) but always reverse-proxied to `127.0.0.1:8083`.

## Data ownership

### Browser-local (SQLite WASM) — cache only

| Collection | Purpose |
|------------|---------|
| `meta` | Dataset version / import timestamp |
| `holidays` | Cached rows: date, month, day, name, category, country, … |
| (optional) `prefs_cache` | Last-seen non-secret UI prefs for offline display |

**Never store:** password hashes, session tokens, API key plaintext/hashes used for auth, Stripe customer IDs as secrets, email-provider keys, or any billing source of truth.

### Server-authoritative — DuckDB (`holidays.duckdb`)

| Collection | Purpose |
|------------|---------|
| `holidays` | Full observances dataset for API and digest generation |
| (optional) import/`meta` | Version matching `holidays.json` |

### Server-authoritative — SQLite (`accounts.db`)

| Collection | Purpose |
|------------|---------|
| `users` | Account identity, email, password hash or magic-link state |
| `sessions` | Server sessions |
| `email_prefs` | Digest on/off, IANA timezone, category filters |
| `api_keys` | Key id, hash, label, created/revoked, rate-tier hints |
| `subscriptions` | Stripe customer/subscription ids, plan, status, period end |

## Consequences

- Anonymous static browse stays intact; WASM improves offline/query UX without becoming a trust boundary.
- Member features require the gin service; deploy is static rsync **plus** binary/unit updates.
- Dual embedded DBs avoid Postgres ops cost while separating OLAP-ish holiday reads (DuckDB) from OLTP auth/billing (SQLite).
- Issue [#3](https://github.com/james-see/everydayisaholiday/issues/3) implements client cache only; [#4](https://github.com/james-see/everydayisaholiday/issues/4) stands up gin + systemd + Caddy routes + `accounts.db`.
