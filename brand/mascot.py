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

SHADOW = f'<ellipse cx="100" cy="184" rx="80" ry="6" fill="{NAVY}" opacity=".12"/>'

HEAD_PATH = "M38 74 C38 36 66 14 100 14 C134 14 162 36 162 74 C162 100 144 126 100 127 C56 126 38 100 38 74 Z"

# Six arms fan out from under the head, each sweeping outward and hooking away from the centre. The inner pair
# stay clear of each other, so nothing in the middle can read as a cleft.
LIMBS = [
    "M56 108 C38 114 30 128 31 142 C32 151 24 156 17 151",      # left, outer
    "M72 120 C60 134 58 148 64 160 C67 167 59 174 51 169",      # left, middle
    "M90 124 C86 138 88 150 93 160 C96 166 91 173 83 170",      # left, inner
    "M144 108 C162 114 170 128 169 142 C168 151 176 156 183 151",  # right, outer
    "M128 120 C140 134 142 148 136 160 C133 167 141 174 149 169",  # right, middle
    "M110 124 C114 138 112 150 107 160 C104 166 109 173 117 170",  # right, inner
]
LIMB_WIDTH = 17

LOWER = "".join(tube(d, LIMB_WIDTH) for d in LIMBS)

def halo():
    """A white sticker edge around the whole figure: it also keeps the outline readable on dark surfaces."""
    edge = "".join(f'<path d="{d}" fill="none" stroke="#fff" stroke-width="{LIMB_WIDTH + 14}" stroke-linecap="round" stroke-linejoin="round"/>' for d in LIMBS)
    return edge + f'<path d="{HEAD_PATH}" fill="#fff" stroke="#fff" stroke-width="11" stroke-linejoin="round"/>'

HEAD = (
    f'<path d="{HEAD_PATH}" fill="{PINK}" stroke="{NAVY}" stroke-width="4" stroke-linejoin="round"/>'
    # shine, and spots on the dome
    f'<path d="M132 30 C146 36 156 48 160 64" fill="none" stroke="{PINK_LIGHT}" stroke-width="7" stroke-linecap="round"/>'
    f'<ellipse cx="62" cy="48" rx="10" ry="6" transform="rotate(-35 62 48)" fill="{PINK_SHADE}"/>'
    f'<circle cx="80" cy="32" r="4" fill="{PINK_SHADE}"/><circle cx="50" cy="66" r="3.2" fill="{PINK_SHADE}"/>'
    f'<circle cx="94" cy="24" r="2.6" fill="{PINK_SHADE}"/>'
)

# Suckers on the limbs, drawn after the head so they stay on the arms.
SUCKERS = (suckers([(26, 148), (33, 140)]) + suckers([(174, 148), (167, 140)]) + suckers([(60, 160), (62, 152)]) + suckers([(140, 160), (138, 152)]))

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
    # name: (raised arms drawn behind the head, face, props drawn in front)
    "multitask": (
        tube("M58 98 C36 100 26 86 30 68") + tube("M142 98 C164 100 174 86 170 68"),
        "open",
        phone(14, 28, -12) + envelope(150, 40, 10) + pencil(14, 160, 40) + mug(154, 138) + laptop(),
    ),
    "investigate": (
        tube("M142 98 C164 94 172 78 164 62"),
        "open",
        document(0, 118, -10) + magnifier(158, 48),
    ),
    "done": (
        tube("M58 96 C36 90 28 70 36 48") + tube("M142 96 C164 90 172 70 164 48"),
        "happy",
        sparkle(20, 36, 1.2, YELLOW) + sparkle(182, 34, 1.2, "#fff") + sparkle(184, 100, 0.8, YELLOW) + sparkle(18, 96, 0.8, "#fff") + check_badge(100, 10),
    ),
    "sleep": (
        "",
        "closed",
        mug(154, 138) +
        f'<g fill="{NAVY}" font-family="ui-rounded, Arial Rounded MT Bold, system-ui, sans-serif" font-weight="800">'
        f'<text x="150" y="30" font-size="22">Z</text><text x="166" y="14" font-size="16">z</text><text x="178" y="4" font-size="11">z</text></g>',
    ),
    "stop": (
        tube("M142 98 C164 94 176 76 168 52"),
        "serious",
        sign_stop(146, 14),
    ),
    "oops": (
        tube("M142 98 C162 96 172 82 166 66"),
        "oops",
        sign_warning(146, 30) +
        f'<path d="M150 62 q6 12 0 16 q-6 -4 0 -16 Z" fill="#8ED4FF" stroke="{NAVY}" stroke-width="2.5" transform="translate(-92 -2)"/>',
    ),
}

OUTLINE_ON_DARK = "#9D98E8"

def svg(pose, background=None, shadow=True, dark=False):
    """dark=True is for dark surfaces: the navy outline would vanish there (contrast 1.2), so strokes turn lavender."""
    arms, face, props = POSES[pose]
    bg = f'<rect width="200" height="200" fill="{background}"/>' if background else ""
    out = (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" role="img" aria-label="Pouple the octopus">'
           f'{bg}{SHADOW if shadow and not dark else ""}{halo()}{arms}{LOWER}{HEAD}{SUCKERS}<g transform="translate(0 -15)">{eyes(face)}</g>{props}</svg>')
    if dark:
        out = out.replace(f'stroke="{NAVY}"', f'stroke="{OUTLINE_ON_DARK}"').replace(f'<g fill="{NAVY}" font-family', f'<g fill="{LAVENDER}" font-family')
    return out

def icon():
    """The app icon: the octopus on a soft lavender squircle (1024 x 1024)."""
    body = f'{halo()}{LOWER}{HEAD}{SUCKERS}<g transform="translate(0 -15)">{eyes("open")}</g>'
    return ('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024" role="img" aria-label="Pouple">'
            '<defs><linearGradient id="g" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#DAD8FF"/><stop offset="1" stop-color="#A8A2FF"/></linearGradient></defs>'
            '<rect x="64" y="64" width="896" height="896" rx="200" fill="url(#g)"/>'
            f'<g transform="translate(72 62) scale(4.4)">{body}</g></svg>')

def favicon():
    """Head only: the arms are too fine at 16px."""
    head = f'<path d="{HEAD_PATH}" fill="#fff" stroke="#fff" stroke-width="11" stroke-linejoin="round"/>{HEAD}<g transform="translate(0 -15)">{eyes("open")}</g>'
    return f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="24 4 152 132">{head}</svg>'

if __name__ == "__main__" and False:  # superseded by pouple.py (the final drawing); kept for its props
    out = pathlib.Path(__file__).parent
    for name in POSES:
        (out / f"mascot-{name}.svg").write_text(svg(name) + "\n")
        (out / f"mascot-{name}-dark.svg").write_text(svg(name, dark=True) + "\n")
    (out / "favicon.svg").write_text(favicon() + "\n")
    (out / "app-icon.svg").write_text(icon() + "\n")
    print("written:", ", ".join(POSES))
