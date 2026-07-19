#!/usr/bin/env python3
"""Fetch National/International/World Day observances from Checkiday and merge into holidays.json."""

from __future__ import annotations

import json
import re
import sys
import time
import urllib.error
import urllib.request
from datetime import date, timedelta
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
HOLIDAYS_PATH = ROOT / "docs" / "holidays.json"
API = "https://www.checkiday.com/api/3/?d={month}/{day}/{year}"
PREFIXES = ("National ", "International ", "World ")
USER_AGENT = "everydayisaholiday/1.0 (+https://adayisaholiday.com; data curation)"
SLEEP_S = 0.08
MAX_RETRIES = 4

# Strip trailing parentheticals like "(US)" for dedupe keys.
PAREN_RE = re.compile(r"\s*\([^)]*\)\s*$")


def normalize_name(name: str) -> str:
    return PAREN_RE.sub("", name).strip().lower()


def dedupe_key(mmdd: str, name: str) -> tuple[str, str]:
    return (mmdd, normalize_name(name))


def fetch_day(d: date) -> list[dict]:
    url = API.format(month=d.month, day=d.day, year=d.year)
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    last_err: Exception | None = None
    for attempt in range(MAX_RETRIES):
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                payload = json.load(resp)
            if payload.get("error") not in (None, "none", ""):
                raise RuntimeError(f"API error for {d}: {payload.get('error')}")
            return payload.get("holidays") or []
        except (
            urllib.error.URLError,
            TimeoutError,
            json.JSONDecodeError,
            RuntimeError,
        ) as e:
            last_err = e
            time.sleep(0.5 * (attempt + 1))
    raise RuntimeError(f"Failed to fetch {d}: {last_err}")


def dates_to_fetch() -> list[date]:
    days: list[date] = []
    d = date(2026, 1, 1)
    end = date(2026, 12, 31)
    while d <= end:
        days.append(d)
        d += timedelta(days=1)
    days.append(date(2024, 2, 29))
    return days


def to_entry(d: date, name: str) -> dict:
    return {
        "date": f"{d.month:02d}-{d.day:02d}",
        "month": d.month,
        "day": d.day,
        "name": name,
        "category": "Secular/Cultural",
    }


def main() -> int:
    with HOLIDAYS_PATH.open(encoding="utf-8") as f:
        existing: list[dict] = json.load(f)

    before = len(existing)
    seen = {dedupe_key(h["date"], h["name"]) for h in existing}

    added: list[dict] = []
    days = dates_to_fetch()
    for i, d in enumerate(days, 1):
        holidays = fetch_day(d)
        for h in holidays:
            name = (h.get("name") or "").strip()
            if not name or not name.startswith(PREFIXES):
                continue
            mmdd = f"{d.month:02d}-{d.day:02d}"
            key = dedupe_key(mmdd, name)
            if key in seen:
                continue
            seen.add(key)
            added.append(to_entry(d, name))
        if i % 30 == 0 or i == len(days):
            print(f"Fetched {i}/{len(days)} days; new so far: {len(added)}", flush=True)
        time.sleep(SLEEP_S)

    merged = existing + added
    merged.sort(key=lambda h: (h["month"], h["day"], h["name"].lower()))

    with HOLIDAYS_PATH.open("w", encoding="utf-8") as f:
        json.dump(merged, f, indent=2, ensure_ascii=False)
        f.write("\n")

    secular = sum(1 for h in merged if h.get("category") == "Secular/Cultural")
    print(f"Before: {before}")
    print(f"Added:  {len(added)}")
    print(f"After:  {len(merged)}")
    print(f"Secular/Cultural: {secular}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
