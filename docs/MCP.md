# Member API & MCP

## REST (`/v1`)

Create a key on [Account](https://adayisaholiday.com/account.html) (shown once).

```bash
curl -sS -H "Authorization: Bearer ah_…" 'https://adayisaholiday.com/v1/today?tz=America/New_York'
curl -sS -H "Authorization: Bearer ah_…" 'https://adayisaholiday.com/v1/holidays/08-17'
curl -sS -H "Authorization: Bearer ah_…" 'https://adayisaholiday.com/v1/holidays?q=cheese&limit=20'
```

Rate limits: **free** 60 req/min, **paid** 600/min (paid tier after Stripe).

Swagger: https://adayisaholiday.com/swagger/index.html

## MCP (remote, OAuth)

- Endpoint: `https://adayisaholiday.com/mcp`
- Auth: OAuth 2.1 authorization code + PKCE (account required)
- Discovery: `/.well-known/oauth-protected-resource`, `/.well-known/oauth-authorization-server`
- DCR: `POST /oauth/register`
- Tools: `holidays_today`, `holidays_by_date`, `holidays_search`
- Scope: `holidays:read`

API keys (`ah_…`) are also accepted as Bearer tokens on `/mcp` for simpler clients.

### Cursor / Claude example

Add a remote MCP server URL `https://adayisaholiday.com/mcp`. Complete the browser OAuth consent when prompted (sign in at adayisaholiday.com first if needed).
