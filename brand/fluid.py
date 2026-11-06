#!/usr/bin/env python3
"""Pouple with flowing tentacles (brand/fluid/<name>.svg): each arm follows a smooth spline with an S-curve and a
curled tip, instead of a single stiff curve. Same construction as fan.py (tapered shapes, head laid over their bases,
no seam)."""
import math, pathlib
import mascot_v1 as v
import fan

NAVY, PINK, SHADE = v.NAVY, v.PINK, v.PINK_SHADE

def spline(points, steps=60):
    """Catmull-Rom through the points (the end points are duplicated so the curve reaches them)."""
    pts = [points[0]] + list(points) + [points[-1]]
    out = []
    n = len(pts) - 3
    for seg in range(n):
        p0, p1, p2, p3 = pts[seg], pts[seg + 1], pts[seg + 2], pts[seg + 3]
        for i in range(steps // n + (1 if seg == n - 1 else 0)):
            t = i / (steps // n)
            t2, t3 = t * t, t * t * t
            out.append(tuple(0.5 * ((2 * p1[k]) + (-p0[k] + p2[k]) * t + (2 * p0[k] - 5 * p1[k] + 4 * p2[k] - p3[k]) * t2 + (-p0[k] + 3 * p1[k] - 3 * p2[k] + p3[k]) * t3) for k in (0, 1)))
    return out

def arm(points, base, tip=6.0, wave=0.0, phase=0.0):
    centre = spline(points)
    n = len(centre)
    left, right, info = [], [], []
    for i, (x, y) in enumerate(centre):
        a, b = centre[max(0, i - 1)], centre[min(n - 1, i + 1)]
        dx, dy = b[0] - a[0], b[1] - a[1]
        d = math.hypot(dx, dy) or 1
        nx, ny = -dy / d, dx / d
        t = i / (n - 1)
        if wave:
            # an extra gentle wiggle, strongest in the middle of the arm
            shift = wave * math.sin(2 * math.pi * 1.25 * t + phase) * math.sin(math.pi * t)
            x, y = x + nx * shift, y + ny * shift
        half = (base / 2) * (1 - t) ** 0.7 + (tip / 2) * t
        left.append((x + nx * half, y + ny * half))
        right.append((x - nx * half, y - ny * half))
        info.append((x, y, nx, ny, half))
    ring = left + right[::-1]
    return "M" + " L".join(f"{x:.1f} {y:.1f}" for x, y in ring) + " Z", info

def figure(arms, wave=0.0):
    paths, dots = [], []
    for k, (points, base) in enumerate(arms):
        d, info = arm(points, base, wave=wave, phase=k * 1.3)
        paths.append(f'<path d="{d}" fill="{PINK}" stroke="{NAVY}" stroke-width="4" stroke-linejoin="round"/>')
        mid = info[len(info) // 2]
        side = 1 if mid[3] >= 0 else -1          # the lower edge, decided once for the whole arm
        for f in (0.30, 0.42, 0.54, 0.66, 0.78):
            x, y, nx, ny, half = info[int(f * (len(info) - 1))]
            dots.append(f'<circle cx="{x + nx * side * half * 0.3:.1f}" cy="{y + ny * side * half * 0.3:.1f}" r="{max(0.9, half * 0.3):.1f}" fill="{SHADE}"/>')
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" role="img" aria-label="Pouple the octopus">'
            f'<ellipse cx="100" cy="190" rx="86" ry="6" fill="{NAVY}" opacity=".12"/>' + "".join(paths)
            + f'<path d="{fan.HEAD_PATH}" fill="{PINK}"/>'
            + f'<path d="{fan.HEAD_OUTLINE}" fill="none" stroke="{NAVY}" stroke-width="4" stroke-linecap="round" stroke-linejoin="round"/>'
            + fan.HEAD_DETAILS + "".join(dots) + v.eyes("open") + "</svg>")

# right side, outermost first
RIGHT = [
    ([(116, 116), (140, 110), (162, 116), (180, 110), (192, 96), (189, 84)], 28),
    ([(118, 124), (142, 128), (160, 138), (176, 148), (187, 144), (190, 133)], 28),
    ([(114, 130), (134, 138), (150, 152), (158, 168), (170, 177), (181, 172)], 26),
    ([(106, 132), (118, 148), (124, 164), (132, 178), (144, 185), (153, 177)], 22),
]
# the left side is not a mirror: arms differ in length and in where they curl
LEFT = [
    ([(84, 116), (60, 112), (40, 121), (24, 116), (12, 102), (14, 91)], 28),
    ([(82, 124), (58, 131), (40, 143), (26, 151), (16, 146), (13, 135)], 28),
    ([(86, 130), (68, 142), (55, 157), (51, 171), (39, 179), (29, 172)], 26),
    ([(94, 132), (84, 150), (79, 167), (69, 179), (57, 183), (52, 174)], 22),
]

def interleave(a, b):
    out = []
    for x, y in zip(a, b):
        out += [x, y]
    return out

def calm(points):
    return points[:-1]

VARIANTS = {
    "fluid-a": lambda: figure(interleave(RIGHT, LEFT)),
    "fluid-b": lambda: figure(interleave(RIGHT, LEFT), wave=2.4),
    "fluid-c": lambda: figure(interleave([(calm(p), b) for p, b in RIGHT], [(calm(p), b) for p, b in LEFT])),
}

if __name__ == "__main__":
    out = pathlib.Path(__file__).parent / "fluid"
    out.mkdir(exist_ok=True)
    for name, fn in VARIANTS.items():
        (out / f"{name}.svg").write_text(fn() + "\n")
    print("written:", ", ".join(VARIANTS))
