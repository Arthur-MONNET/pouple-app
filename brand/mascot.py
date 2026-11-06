#!/usr/bin/env python3
"""Draws Pouple the octopus in several poses as standalone SVG files (brand/mascot-<pose>.svg).

Everything is drawn in layers on a 200x200 canvas: shadow, lower tentacles, arms, head, face, props. Arms are drawn
behind the head so only the part that leaves the body shows; each stroke is drawn twice (a wide dark one under a
narrower pink one) to get a clean outline with round joints.
"""
import pathlib

PINK, PINK_SHADE, PINK_LIGHT = "#F2668B", "#D94A74", "#FF9DB8"
NAVY = "#231F52"
LAVENDER = "#C9C6FF"
GREEN, YELLOW, BLUE, RED = "#4CC38A", "#FFC857", "#5B7CFF", "#EF5B5B"
PAPER = "#FFFFFF"
SCREEN = "#D9DDFB"

def tube(d, width=15, color=PINK):
    """A thick line with an outline."""
    return (f'<path d="{d}" fill="none" stroke="{NAVY}" stroke-width="{width + 6}" stroke-linecap="round" stroke-linejoin="round"/>'
            f'<path d="{d}" fill="none" stroke="{color}" stroke-width="{width}" stroke-linecap="round" stroke-linejoin="round"/>')

def suckers(points, r=2.2):
    return "".join(f'<circle cx="{x}" cy="{y}" r="{r}" fill="{PINK_SHADE}"/>' for x, y in points)

SHADOW = f'<ellipse cx="100" cy="190" rx="78" ry="6" fill="{NAVY}" opacity=".12"/>'

# One silhouette: a wide dome that flows straight into two broad centre arms (meeting in a V) and two short outer
# arms that flare out and curl up. No neck, no flat base: the arms are part of the head, as in a sticker.
BODY_PATH = (
    "M36 100 C30 54 58 14 100 14 C142 14 170 54 164 100 "
    "C170 112 186 116 192 126 C195 136 184 142 172 138 "
    "C166 154 146 166 124 164 C112 163 104 156 100 140 "
    "C96 156 88 163 76 164 C54 166 34 154 28 138 "
    "C16 142 5 136 8 126 C14 116 30 112 36 100 Z"
)

LOWER = ""  # kept for the icon: the arms are now part of BODY

def halo():
    """A white sticker edge around the body: it also keeps the outline readable on dark surfaces."""
    return f'<path d="{BODY_PATH}" fill="#fff" stroke="#fff" stroke-width="11" stroke-linejoin="round"/>'

HEAD = (
    f'<path d="{BODY_PATH}" fill="{PINK}" stroke="{NAVY}" stroke-width="4" stroke-linejoin="round"/>'
    # folds that separate the arms
    f'<path d="M128 124 C130 142 120 155 108 158" fill="none" stroke="{NAVY}" stroke-width="3" stroke-linecap="round"/>'
    f'<path d="M72 124 C70 142 80 155 92 158" fill="none" stroke="{NAVY}" stroke-width="3" stroke-linecap="round"/>'
    f'<path d="M176 122 C181 127 181 132 178 136" fill="none" stroke="{NAVY}" stroke-width="3" stroke-linecap="round"/>'
    f'<path d="M24 122 C19 127 19 132 22 136" fill="none" stroke="{NAVY}" stroke-width="3" stroke-linecap="round"/>'
    # shine, and spots on the dome
    f'<path d="M132 34 C146 40 156 52 160 68" fill="none" stroke="{PINK_LIGHT}" stroke-width="7" stroke-linecap="round"/>'
    f'<ellipse cx="62" cy="52" rx="10" ry="6" transform="rotate(-35 62 52)" fill="{PINK_SHADE}"/>'
    f'<circle cx="80" cy="36" r="4" fill="{PINK_SHADE}"/><circle cx="50" cy="70" r="3.2" fill="{PINK_SHADE}"/>'
    f'<circle cx="94" cy="28" r="2.6" fill="{PINK_SHADE}"/>'
    f'{suckers([(150, 150), (142, 157), (50, 150), (58, 157)], 2.2)}'
)

def eyes(kind):
    # Big, wide-set eyes in the lower half of the dome: it is what makes the head read as friendly.
    cheeks = (f'<ellipse cx="52" cy="112" rx="8" ry="5" fill="{PINK_LIGHT}"/>'
              f'<ellipse cx="148" cy="112" rx="8" ry="5" fill="{PINK_LIGHT}"/>')
    def open_eyes(r=11.5):
        return (f'<circle cx="74" cy="98" r="{r}" fill="{NAVY}"/><circle cx="126" cy="98" r="{r}" fill="{NAVY}"/>'
                f'<circle cx="78" cy="93.5" r="4" fill="#fff"/><circle cx="130" cy="93.5" r="4" fill="#fff"/>'
                f'<circle cx="71" cy="103" r="2" fill="#fff"/><circle cx="123" cy="103" r="2" fill="#fff"/>')
    smile = f'<path d="M91 114 Q100 128 109 114 Z" fill="#B83A64" stroke="{NAVY}" stroke-width="3" stroke-linejoin="round"/>'
    if kind == "happy":
        arcs = "".join(f'<path d="M{x - 11} 102 Q{x} 84 {x + 11} 102" fill="none" stroke="{NAVY}" stroke-width="5" stroke-linecap="round"/>' for x in (74, 126))
        grin = f'<path d="M86 112 Q100 134 114 112 Z" fill="#B83A64" stroke="{NAVY}" stroke-width="3" stroke-linejoin="round"/>'
        return cheeks + arcs + grin
    if kind == "closed":
        arcs = "".join(f'<path d="M{x - 11} 98 Q{x} 110 {x + 11} 98" fill="none" stroke="{NAVY}" stroke-width="5" stroke-linecap="round"/>' for x in (74, 126))
        return cheeks + arcs + f'<path d="M94 118 Q100 122 106 118" fill="none" stroke="{NAVY}" stroke-width="3.5" stroke-linecap="round"/>'
    if kind == "oops":
        return (cheeks + open_eyes(9) +
                f'<path d="M60 80 Q74 70 88 80" fill="none" stroke="{NAVY}" stroke-width="3.5" stroke-linecap="round"/>'
                f'<path d="M112 80 Q126 70 140 80" fill="none" stroke="{NAVY}" stroke-width="3.5" stroke-linecap="round"/>'
                f'<path d="M90 120 Q95 114 100 120 T110 120" fill="none" stroke="{NAVY}" stroke-width="3.5" stroke-linecap="round"/>')
    if kind == "serious":
        return (cheeks + open_eyes() +
                f'<path d="M60 82 L88 87" stroke="{NAVY}" stroke-width="4" stroke-linecap="round"/>'
                f'<path d="M140 82 L112 87" stroke="{NAVY}" stroke-width="4" stroke-linecap="round"/>'
                f'<path d="M93 119 L107 119" stroke="{NAVY}" stroke-width="3.5" stroke-linecap="round"/>')
    return cheeks + open_eyes() + smile

def laptop():
    return (f'<path d="M64 124 Q64 118 70 118 L130 118 Q136 118 136 124 L138 152 L62 152 Z" fill="{SCREEN}" stroke="{NAVY}" stroke-width="4" stroke-linejoin="round"/>'
            f'<ellipse cx="100" cy="134" rx="6.5" ry="5" fill="#fff"/>'
            f'<path d="M54 152 L146 152 Q150 152 149 157 Q148 162 142 162 L58 162 Q52 162 51 157 Q50 152 54 152 Z" fill="#B9BFEF" stroke="{NAVY}" stroke-width="4" stroke-linejoin="round"/>')

def phone(x, y, rot):
    return (f'<g transform="rotate({rot} {x + 14} {y + 24})">'
            f'<rect x="{x}" y="{y}" width="28" height="48" rx="7" fill="{BLUE}" stroke="{NAVY}" stroke-width="4"/>'
            f'<rect x="{x + 5}" y="{y + 6}" width="18" height="30" rx="3" fill="#BFD0FF"/>'
            f'<circle cx="{x + 14}" cy="{y + 42}" r="2" fill="{NAVY}"/></g>')

def envelope(x, y, rot):
    return (f'<g transform="rotate({rot} {x + 18} {y + 13})">'
            f'<rect x="{x}" y="{y}" width="36" height="26" rx="3" fill="{PAPER}" stroke="{NAVY}" stroke-width="4" stroke-linejoin="round"/>'
            f'<path d="M{x + 2} {y + 3} L{x + 18} {y + 16} L{x + 34} {y + 3}" fill="none" stroke="{NAVY}" stroke-width="3.5" stroke-linejoin="round" stroke-linecap="round"/></g>')

def mug(x, y):
    return (f'<path d="M{x + 5} {y - 8} q-4 -6 0 -10 M{x + 14} {y - 8} q-4 -6 0 -10" fill="none" stroke="#fff" stroke-width="3" stroke-linecap="round" opacity=".9"/>'
            f'<path d="M{x + 28} {y + 4} q10 0 10 9 q0 9 -10 9" fill="none" stroke="{NAVY}" stroke-width="4" stroke-linecap="round"/>'
            f'<rect x="{x}" y="{y}" width="30" height="30" rx="6" fill="{GREEN}" stroke="{NAVY}" stroke-width="4"/>'
            f'<ellipse cx="{x + 15}" cy="{y + 15}" rx="6" ry="7" fill="#fff" opacity=".9"/>')

def pencil(x, y, rot):
    return (f'<g transform="rotate({rot} {x} {y})">'
            f'<rect x="{x - 5}" y="{y - 32}" width="10" height="34" rx="2" fill="{YELLOW}" stroke="{NAVY}" stroke-width="3.5" stroke-linejoin="round"/>'
            f'<path d="M{x - 5} {y + 2} L{x} {y + 13} L{x + 5} {y + 2} Z" fill="#F4D9B0" stroke="{NAVY}" stroke-width="3.5" stroke-linejoin="round"/>'
            f'<rect x="{x - 5}" y="{y - 32}" width="10" height="6" rx="2" fill="#FF8FAE" stroke="{NAVY}" stroke-width="3.5"/></g>')

def magnifier(x, y):
    return (f'<path d="M{x + 14} {y + 14} L{x + 34} {y + 36}" stroke="{NAVY}" stroke-width="11" stroke-linecap="round"/>'
            f'<path d="M{x + 14} {y + 14} L{x + 34} {y + 36}" stroke="#8A6A4F" stroke-width="5" stroke-linecap="round"/>'
            f'<circle cx="{x}" cy="{y}" r="22" fill="#fff" fill-opacity=".55" stroke="{NAVY}" stroke-width="5"/>'
            f'<path d="M{x - 13} {y - 6} Q{x - 8} {y - 14} {x} {y - 14}" fill="none" stroke="#fff" stroke-width="4" stroke-linecap="round"/>')

def document(x, y, rot):
    return (f'<g transform="rotate({rot} {x + 16} {y + 22})">'
            f'<rect x="{x}" y="{y}" width="32" height="44" rx="4" fill="{PAPER}" stroke="{NAVY}" stroke-width="4"/>'
            f'<path d="M{x + 7} {y + 12} h18 M{x + 7} {y + 21} h18 M{x + 7} {y + 30} h11" stroke="{BLUE}" stroke-width="3.5" stroke-linecap="round"/></g>')

def sign_stop(x, y):
    pts = "13,0 31,0 44,13 44,31 31,44 13,44 0,31 0,13"
    return (f'<g transform="translate({x} {y}) rotate(8 22 22)"><polygon points="{pts}" fill="{RED}" stroke="{NAVY}" stroke-width="4" stroke-linejoin="round"/>'
            f'<rect x="14" y="14" width="16" height="16" rx="3" fill="#fff"/></g>')

def sign_warning(x, y):
    return (f'<g transform="translate({x} {y}) rotate(-8 20 20)"><path d="M20 2 L40 36 L0 36 Z" fill="{YELLOW}" stroke="{NAVY}" stroke-width="4" stroke-linejoin="round"/>'
            f'<path d="M20 14 V26" stroke="{NAVY}" stroke-width="4" stroke-linecap="round"/><circle cx="20" cy="31" r="2.2" fill="{NAVY}"/></g>')

def sparkle(x, y, s=1, color="#fff"):
    return f'<path d="M{x} {y - 8 * s} Q{x} {y} {x + 8 * s} {y} Q{x} {y} {x} {y + 8 * s} Q{x} {y} {x - 8 * s} {y} Q{x} {y} {x} {y - 8 * s} Z" fill="{color}" stroke="{NAVY}" stroke-width="2" stroke-linejoin="round"/>'

def check_badge(x, y):
    return (f'<circle cx="{x}" cy="{y}" r="15" fill="{GREEN}" stroke="{NAVY}" stroke-width="4"/>'
            f'<path d="M{x - 7} {y + 1} L{x - 2} {y + 6} L{x + 8} {y - 6}" fill="none" stroke="#fff" stroke-width="4.5" stroke-linecap="round" stroke-linejoin="round"/>')

POSES = {
    # name: (arms drawn behind the head, face, props drawn in front)
    "multitask": (
        tube("M56 112 C34 112 26 98 30 82") + tube("M144 112 C166 112 174 98 170 82") +
        tube("M52 126 C34 134 28 146 34 156") + tube("M148 126 C168 134 174 146 168 156"),
        "open",
        phone(14, 38, -12) + envelope(148, 56, 10) + pencil(36, 160, 35) + mug(158, 138) + laptop() +
        suckers([(34, 104), (36, 96)]),
    ),
    "investigate": (
        tube("M144 112 C164 108 172 92 164 76") + tube("M56 118 C38 122 30 134 34 146"),
        "open",
        document(10, 124, -10) + magnifier(158, 62) + suckers([(36, 150)]),
    ),
    "done": (
        tube("M56 108 C34 100 26 80 34 58") + tube("M144 108 C166 100 174 80 166 58") +
        tube("M60 128 C46 140 44 152 52 160") + tube("M140 128 C154 140 156 152 148 160"),
        "happy",
        sparkle(20, 42, 1.2, YELLOW) + sparkle(182, 40, 1.2, "#fff") + sparkle(180, 108, 0.8, YELLOW) + sparkle(24, 100, 0.8, "#fff") + check_badge(100, 12),
    ),
    "sleep": (
        tube("M56 120 C40 128 34 142 40 152") + tube("M144 124 C158 124 162 132 160 142"),
        "closed",
        mug(150, 138).replace('opacity=".9"', 'opacity=".9"') +
        f'<g fill="{NAVY}" font-family="ui-rounded, Arial Rounded MT Bold, system-ui, sans-serif" font-weight="800">'
        f'<text x="150" y="38" font-size="22">Z</text><text x="166" y="22" font-size="16">z</text><text x="178" y="10" font-size="11">z</text></g>',
    ),
    "stop": (
        tube("M144 112 C166 108 176 90 168 66") + tube("M56 124 C38 130 32 144 38 154"),
        "serious",
        sign_stop(146, 22),
    ),
    "oops": (
        tube("M144 112 C164 110 172 96 166 80") + tube("M56 122 C38 124 28 138 34 150"),
        "oops",
        sign_warning(146, 40) +
        f'<path d="M150 62 q6 12 0 16 q-6 -4 0 -16 Z" fill="#8ED4FF" stroke="{NAVY}" stroke-width="2.5" transform="translate(-92 -2)"/>'
        f'<path d="M30 120 q4 8 0 12 q-4 -4 0 -12 Z" fill="#8ED4FF" stroke="{NAVY}" stroke-width="2.5" opacity="0"/>',
    ),
}

OUTLINE_ON_DARK = "#9D98E8"

def svg(pose, background=None, shadow=True, dark=False):
    """dark=True is for dark surfaces: the navy outline would vanish there (contrast 1.2), so strokes turn lavender."""
    arms, face, props = POSES[pose]
    bg = f'<rect width="200" height="200" fill="{background}"/>' if background else ""
    out = (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" role="img" aria-label="Pouple the octopus">'
           f'{bg}{SHADOW if shadow and not dark else ""}{halo()}{arms}{HEAD}<g transform="translate(0 -15)">{eyes(face)}</g>{props}</svg>')
    if dark:
        out = out.replace(f'stroke="{NAVY}"', f'stroke="{OUTLINE_ON_DARK}"').replace(f'<g fill="{NAVY}" font-family', f'<g fill="{LAVENDER}" font-family')
    return out

def icon():
    """The app icon: the octopus on a soft lavender squircle (1024 x 1024)."""
    body = f'{halo()}{HEAD}<g transform="translate(0 -15)">{eyes("open")}</g>'
    return ('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024" role="img" aria-label="Pouple">'
            '<defs><linearGradient id="g" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#DAD8FF"/><stop offset="1" stop-color="#A8A2FF"/></linearGradient></defs>'
            '<rect x="64" y="64" width="896" height="896" rx="200" fill="url(#g)"/>'
            f'<g transform="translate(72 46) scale(4.4)">{body}</g></svg>')

if __name__ == "__main__":
    out = pathlib.Path(__file__).parent
    for name in POSES:
        (out / f"mascot-{name}.svg").write_text(svg(name) + "\n")
        (out / f"mascot-{name}-dark.svg").write_text(svg(name, dark=True) + "\n")
    (out / "app-icon.svg").write_text(icon() + "\n")
    print("written:", ", ".join(POSES))
