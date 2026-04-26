#!/usr/bin/env python3
"""
Render octant terminal output to a PNG using the actual CaskaydiaMono font,
simulating exactly what the terminal displays.

Usage:
    python3 render_screenshot.py [image_file] [output_png]
    python3 render_screenshot.py 1.jpg screenshot.png

Defaults: image_file=1.jpg, output_png=screenshot.png
"""

import sys
import subprocess
import re
from PIL import Image, ImageDraw, ImageFont

FONT_PATH = "/home/reynolds/.local/share/fonts/CascadiaMono/CaskaydiaMonoNerdFont-Regular.ttf"
FONT_PT   = 20   # gives 12×24px cells (1:2 aspect ratio — matches 2×4 octant source pixels)

def parse_ansi(data: bytes):
    """
    Parse ANSI truecolor escape sequences and yield rows of (char, fg, bg) tuples.
    fg/bg are (R, G, B) tuples.
    """
    rows = []
    row  = []
    fg   = (255, 255, 255)
    bg   = (0,   0,   0  )

    i = 0
    while i < len(data):
        b = data[i]

        if b == 0x0A:               # newline
            rows.append(row)
            row = []
            fg  = (255, 255, 255)
            bg  = (0,   0,   0  )
            i  += 1
            continue

        if b == 0x1B and i + 1 < len(data) and data[i+1] == ord('['):
            # Scan to the escape sequence terminator (a letter).
            j = i + 2
            while j < len(data) and not chr(data[j]).isalpha():
                j += 1
            terminator = chr(data[j]) if j < len(data) else ''
            seq = data[i+2:j].decode('ascii', errors='replace')
            i   = j + 1

            if terminator == 'A':
                # Cursor-up: signals the start of a new animation frame.
                # Stop here so we capture only the first frame.
                break

            if terminator != 'm':
                # Ignore other non-colour escape sequences (cursor hide, etc.)
                continue

            if seq == '0' or seq == '':   # reset
                fg = (255, 255, 255)
                bg = (0,   0,   0  )
            else:
                parts = seq.split(';')
                k = 0
                while k < len(parts):
                    p = parts[k]
                    if p == '38' and k+4 < len(parts) and parts[k+1] == '2':
                        fg = (int(parts[k+2]), int(parts[k+3]), int(parts[k+4]))
                        k += 5
                    elif p == '48' and k+4 < len(parts) and parts[k+1] == '2':
                        bg = (int(parts[k+2]), int(parts[k+3]), int(parts[k+4]))
                        k += 5
                    else:
                        k += 1
            continue

        # Decode one UTF-8 codepoint
        if b & 0x80 == 0:
            ch = chr(b); sz = 1
        elif b & 0xE0 == 0xC0 and i+1 < len(data):
            ch = chr((b & 0x1F) << 6 | (data[i+1] & 0x3F)); sz = 2
        elif b & 0xF0 == 0xE0 and i+2 < len(data):
            ch = chr((b & 0x0F) << 12 | (data[i+1] & 0x3F) << 6 | (data[i+2] & 0x3F)); sz = 3
        elif b & 0xF8 == 0xF0 and i+3 < len(data):
            ch = chr((b & 0x07) << 18 | (data[i+1] & 0x3F) << 12 |
                     (data[i+2] & 0x3F) << 6  |  (data[i+3] & 0x3F)); sz = 4
        else:
            ch = '?'; sz = 1

        row.append((ch, fg, bg))
        i += sz

    if row:
        rows.append(row)
    return rows


def render(rows, font, cell_w, cell_h, asc, out_path):
    if not rows:
        print("No rows to render.", file=sys.stderr)
        return

    num_cols = max(len(r) for r in rows)
    num_rows = len(rows)
    img_w    = num_cols * cell_w
    img_h    = num_rows * cell_h

    img  = Image.new("RGB", (img_w, img_h), (255, 255, 255))
    draw = ImageDraw.Draw(img)

    for ry, row in enumerate(rows):
        for cx, (ch, fg, bg) in enumerate(row):
            px = cx * cell_w
            py = ry * cell_h
            # Background fill
            draw.rectangle([px, py, px + cell_w - 1, py + cell_h - 1], fill=bg)
            # Character glyph: anchor "ls" places the baseline at (px, py+asc),
            # so the glyph's ascent region starts at cell top.
            if ch not in (' ', '\u00A0', '\u0000'):
                draw.text((px, py + asc), ch, font=font, fill=fg, anchor="ls")

    img.save(out_path)
    print(f"Saved {out_path}  ({img_w}×{img_h}px,  {num_cols}×{num_rows} cells)",
          file=sys.stderr)


def main():
    image_file = sys.argv[1] if len(sys.argv) > 1 else "1.jpg"
    out_png    = sys.argv[2] if len(sys.argv) > 2 else "screenshot.png"

    octant_bin = "/home/reynolds/code/octant/octant"
    try:
        result = subprocess.run([octant_bin, image_file], capture_output=True, timeout=10)
    except subprocess.TimeoutExpired as e:
        # Animated GIF: capture whatever was output before the timeout
        # (the first frame is sufficient for a screenshot).
        result = e
        if not result.stdout:
            print("octant timed out with no output", file=sys.stderr)
            sys.exit(1)
    if hasattr(result, 'returncode') and result.returncode != 0:
        print("octant failed:", result.stderr.decode(), file=sys.stderr)
        sys.exit(1)

    font  = ImageFont.truetype(FONT_PATH, FONT_PT)

    # Determine cell dimensions from the full-block glyph
    bb = font.getbbox("█")
    cell_w = bb[2] - bb[0]
    cell_h = bb[3] - bb[1]
    asc, _desc = font.getmetrics()
    print(f"Font: {FONT_PT}pt  cell: {cell_w}×{cell_h}px  ascent: {asc}px", file=sys.stderr)

    rows = parse_ansi(result.stdout)
    render(rows, font, cell_w, cell_h, asc, out_png)


if __name__ == "__main__":
    main()
