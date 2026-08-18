# API

Go gin service for auth (and later email, member API, Stripe).

## Local

```bash
cd api
cp .env.example .env
# edit .env: COOKIE_SECURE=false, EXPOSE_VERIFY_TOKEN=true for local testing
mkdir -p data
go run ./cmd/server
```

Swagger: http://127.0.0.1:8083/swagger/index.html

## Build (linux amd64 for VPS)

```bash
cd api
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/adayisaholiday-api ./cmd/server
```

## VPS layout

- Binary: `/home/jc/adayisaholidaycom/bin/adayisaholiday-api`
- Env: `/home/jc/adayisaholidaycom/.env` (mode 600)
- DB: `/home/jc/adayisaholidaycom/data/accounts.db`
- Unit: `~/.config/systemd/user/adayisaholiday-api.service`
