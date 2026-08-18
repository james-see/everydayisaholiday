# Every Day is a Holiday

A website celebrating that every day of the year is a holiday somewhere — across all eras, traditions, and cultures.

**Live:** https://adayisaholiday.com

## Overview

A perpetual calendar of **4,076** observances covering 366 days, drawing from:
- Secular/cultural National Days (National Cheese Day, etc.)
- Ancient Roman, Greek, Egyptian, Mesopotamian festivals
- Celtic/Wiccan sabbats (Wheel of the Year)
- Catholic/Orthodox saints' feast days
- Hindu, Buddhist, Sikh, Jain, Zoroastrian, Bahá'í, Shinto observances
- Secular/UN international days
- National independence days across all nations
- Historical and cultural commemorations

## Data

- Dataset: [`docs/holidays.json`](docs/holidays.json)
- Refresh National/International/World Days from Checkiday:

```bash
python3 scripts/fetch_checkiday.py
```

## Architecture

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md): browser SQLite WASM for holiday cache/offline; VPS DuckDB + SQLite; Go gin systemd API on `127.0.0.1:8083` behind Caddy. No Postgres.

Member accounts: [`docs/account.html`](docs/account.html) · API: [`api/`](api/)

## Status

Live on the VPS at adayisaholiday.com (Caddy). Push to `main` deploys `docs/` and the gin API via GitHub Actions.
