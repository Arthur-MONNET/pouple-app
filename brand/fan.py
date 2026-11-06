#!/usr/bin/env python3
"""Pouple with a fan of tapered tentacles that grow out of the head without a seam (brand/fan/<name>.svg).

Each tentacle is a filled leaf-like shape (wide at the base, pointed at the tip) built from a centre line. They are
drawn back to front with their own outline, then the head is laid over their bases: its fill hides the junction and
its outline stops before the bottom, so no line separates the head from the arms.
"""
import math, pathlib
import mascot_v1 as v

NAVY, PINK, SHADE, LIGHT = v.NAVY, v.PINK, v.PINK_SHADE, v.PINK_LIGHT

def bezier(p0, p1, p2, p3, t):
    u = 1 - t
    return (u**3 * p0[0] + 3 * u * u * t * p1[0] + 3 * u * t * t * p2[0] + t**3 * p3[0],
            u**3 * p0[1] + 3 * u * u * t * p1[1] + 3 * u * t * t * p2[1] + t**3 * p3[1])

def tentacle(points, base, tip=5.5, steps=28):
    """Returns (outline path, centre samples, normals) of a tentacle that tapers from `base` to `tip` half-width."""
    p0, p1, p2, p3 = points
    centre, left, right = [], [], []
    for i in range(steps + 1):
        t = i / steps
        x, y = bezier(p0, p1, p2, p3, t)
        e = 0.001
        x2, y2 = bezier(p0, p1, p2, p3, min(1, t + e))
        x1, y1 = bezier(p0, p1, p2, p3, max(0, t - e))
        dx, dy = x2 - x1, y2 - y1
        n = math.hypot(dx, dy) or 1
        nx, ny = -dy / n, dx / n
        half = (base / 2) * (1 - t) ** 0.7 + (tip / 2) * t
        centre.append((x, y, nx, ny, half))
        left.append((x + nx * half, y + ny * half))
        right.append((x - nx * half, y - ny * half))
    ring = left + right[::-1]
    d = "M" + " L".join(f"{x:.1f} {y:.1f}" for x, y in ring) + " Z"
    return d, centre

def flip(points):
    return [(200 - x, y) for x, y in points]

def render(tentacles, name):
    """tentacles: list of (control points, base width); drawn in the given order (back first)."""
    layers, details = [], []
    for points, base in tentacles:
        d, centre = tentacle(points, base)
        layers.append(f'<path d="{d}" fill="{PINK}" stroke="{NAVY}" stroke-width="4" stroke-linejoin="round"/>')
        # suckers along the underside, a light edge along the top
        for k in (0.34, 0.46, 0.58, 0.70, 0.8):
            x, y, nx, ny, half = centre[int(k * 28)]
            sign = 1 if ny >= 0 else -1  # towards the lower edge
            r = max(1.0, half * 0.34)
            details.append(f'<circle cx="{x + nx * sign * half * 0.28:.1f}" cy="{y + ny * sign * half * 0.28:.1f}" r="{r:.1f}" fill="{SHADE}"/>')
    head_fill = f'<path d="{v.HEAD_PATH}" fill="{PINK}"/>' if hasattr(v, "HEAD_PATH") else ""
    return layers, details

HEAD_PATH = "M42 88 C42 44 68 22 100 22 C132 22 158 44 158 88 C158 112 146 130 130 138 C116 144 84 144 70 138 C54 130 42 112 42 88 Z"
# the outline of the head stops before its base, where the arms take over
HEAD_OUTLINE = "M47 116 C43 106 42 98 42 88 C42 44 68 22 100 22 C132 22 158 44 158 88 C158 98 157 106 153 116"
HEAD_DETAILS = v.HEAD.split("/>", 1)[1]

def figure(tentacles):
    layers, details = render(tentacles, "")
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" role="img" aria-label="Pouple the octopus">'
            f'<ellipse cx="100" cy="188" rx="86" ry="6" fill="{NAVY}" opacity=".12"/>'
            + "".join(layers)
            + f'<path d="{HEAD_PATH}" fill="{PINK}"/>'
            + f'<path d="{HEAD_OUTLINE}" fill="none" stroke="{NAVY}" stroke-width="4" stroke-linecap="round" stroke-linejoin="round"/>'
            + HEAD_DETAILS + "".join(details) + v.eyes("open") + "</svg>")

# right-hand tentacles, from the outermost (almost level, tip curling up) to the innermost (pointing down)
RIGHT_8 = [
    (((116, 116), (150, 112), (176, 122), (190, 108)), 30),
    (((118, 124), (152, 128), (174, 142), (186, 140)), 30),
    (((114, 130), (142, 142), (160, 158), (172, 162)), 28),
    (((106, 132), (124, 150), (136, 166), (146, 174)), 24),
]
def mirror(items, shift=0):
    return [(flip(points), base) for points, base in items]

def fan8():
    right = RIGHT_8
    left = mirror(RIGHT_8)
    # draw from the back (outer) to the front (inner), alternating sides so the overlaps look natural
    order = []
    for r, l in zip(right, left):
        order += [r, l]
    return figure(order)

SIX = [RIGHT_8[0], RIGHT_8[2], RIGHT_8[3]]
def fan6():
    order = []
    for r, l in zip(SIX, mirror(SIX)):
        order += [r, l]
    return figure(order)

CURLY_RIGHT = [
    (((116, 116), (152, 108), (182, 114), (190, 94)), 30),
    (((118, 124), (154, 130), (178, 148), (174, 162)), 30),
    (((114, 130), (140, 144), (154, 164), (168, 176)), 28),
    (((106, 132), (116, 152), (130, 170), (122, 184)), 24),
]
CURLY_LEFT = [
    (((84, 116), (48, 112), (22, 122), (10, 110)), 30),
    (((82, 124), (48, 130), (26, 144), (14, 138)), 30),
    (((86, 130), (60, 142), (42, 158), (28, 162)), 28),
    (((94, 132), (78, 150), (66, 168), (54, 176)), 24),
]
def fan_curly():
    order = []
    for r, l in zip(CURLY_RIGHT, CURLY_LEFT):
        order += [r, l]
    return figure(order)

VARIANTS = {"fan-8": fan8, "fan-6": fan6, "fan-curly": fan_curly}

if __name__ == "__main__":
    out = pathlib.Path(__file__).parent / "fan"
    out.mkdir(exist_ok=True)
    for name, fn in VARIANTS.items():
        (out / f"{name}.svg").write_text(fn() + "\n")
    print("written:", ", ".join(VARIANTS))
