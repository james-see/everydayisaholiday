#!/usr/bin/env python3
"""Generate docs/og-card.png (1200x630) from current holidays.json counts."""

from __future__ import annotations

import json
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parents[1]
HOLIDAYS_PATH = ROOT / "docs" / "holidays.json"
OUT_PATH = ROOT / "docs" / "og-card.png"

W, H = 1200, 630
BG = (26, 26, 46)  # --bg
ACCENT = (233, 69, 96)  # --accent
FG = (224, 224, 224)
MUTED = (138, 138, 154)

GEORGIA = "/System/Library/Fonts/Supplemental/Georgia.ttf"
GEORGIA_ITALIC = "/System/Library/Fonts/Supplemental/Georgia Italic.ttf"
ARIAL = "/System/Library/Fonts/Supplemental/Arial.ttf"

TRADITIONS = (
    "Secular · Christian · National · Roman · Pagan · UN · "
    "Hindu · Buddhist · Sikh · Jain · Zoroastrian · Bahá'í · "
    "Shinto · Jewish · Islamic"
)


def font(path: str, size: int) -> ImageFont.FreeTypeFont:
    return ImageFont.truetype(path, size)


def center_text(
    draw: ImageDraw.ImageDraw,
    y: int,
    text: str,
    fnt: ImageFont.ImageFont,
    fill: tuple[int, int, int],
) -> int:
    bbox = draw.textbbox((0, 0), text, font=fnt)
    tw = bbox[2] - bbox[0]
    th = bbox[3] - bbox[1]
    x = (W - tw) // 2
    draw.text((x, y), text, font=fnt, fill=fill)
    return th


def main() -> None:
    with HOLIDAYS_PATH.open(encoding="utf-8") as f:
        holidays = json.load(f)
    count = f"{len(holidays):,}"

    img = Image.new("RGBA", (W, H), BG + (255,))
    draw = ImageDraw.Draw(img)

    # Subtle top accent bar
    draw.rectangle([0, 0, W, 6], fill=ACCENT)

    title_f = font(GEORGIA, 72)
    tag_f = font(GEORGIA_ITALIC, 28)
    stats_f = font(GEORGIA, 36)
    trad_f = font(ARIAL, 18)
    url_f = font(ARIAL, 26)

    y = 140
    center_text(draw, y, "A Day Is a Holiday", title_f, ACCENT)
    y = 240
    center_text(draw, y, "Every day of the year is a holiday somewhere", tag_f, MUTED)
    y = 320
    center_text(
        draw,
        y,
        f"{count} holidays · 366 days · 20+ traditions",
        stats_f,
        FG,
    )
    y = 400
    center_text(draw, y, TRADITIONS, trad_f, MUTED)
    y = 520
    center_text(draw, y, "adayisaholiday.com", url_f, ACCENT)

    img.save(OUT_PATH, "PNG", optimize=True)
    print(f"Wrote {OUT_PATH} ({count} holidays)")


if __name__ == "__main__":
    main()
