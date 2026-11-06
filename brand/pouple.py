#!/usr/bin/env python3
"""The Pouple mascot, final drawing: eight tapered tentacles in a curly fan, one silhouette with the head.

Generates brand/mascot-<pose>.svg, mascot-<pose>-dark.svg, app-icon.svg and favicon.svg. The body comes from
fan.py (CURLY_RIGHT / CURLY_LEFT), the face from the first version (mascot_v1), the props from mascot.py.
"""
import pathlib
import fan
import mascot as m
import mascot_v1 as v

NAVY, PINK, LAVENDER = v.NAVY, v.PINK, v.LAVENDER
OUTLINE_ON_DARK = "#9D98E8"

def body_parts():
    order = []
    for r, l in zip(fan.CURLY_RIGHT, fan.CURLY_LEFT):
        order += [r, l]
    layers, details = fan.render(order, "")
    head = (f'<path d="{fan.HEAD_PATH}" fill="{PINK}"/>'
            f'<path d="{fan.HEAD_OUTLINE}" fill="none" stroke="{NAVY}" stroke-width="4" stroke-linecap="round" stroke-linejoin="round"/>'
            + fan.HEAD_DETAILS + "".join(details))
    return "".join(layers), head

LAYERS, HEAD = body_parts()
SHADOW = f'<ellipse cx="100" cy="190" rx="86" ry="6" fill="{NAVY}" opacity=".12"/>'

# Props that say what the product does: a Slack message comes in, Codex looks into it, a decision is asked, the
# key stays in the keychain. (No real logos: a generic speech bubble and a generic terminal card.)
def chat_bubble(x, y, rot=-8):
    return (f'<g transform="rotate({rot} {x + 20} {y + 16})">'
            f'<path d="M{x + 4} {y} H{x + 36} Q{x + 40} {y} {x + 40} {y + 4} V{y + 24} Q{x + 40} {y + 28} {x + 36} {y + 28} H{x + 14} L{x + 4} {y + 38} V{y + 28} Q{x} {y + 28} {x} {y + 24} V{y + 4} Q{x} {y} {x + 4} {y} Z" fill="#fff" stroke="{NAVY}" stroke-width="4" stroke-linejoin="round"/>'
            f'<text x="{x + 20}" y="{y + 21}" text-anchor="middle" font-family="ui-rounded, Arial Rounded MT Bold, system-ui, sans-serif" font-weight="800" font-size="19" fill="{PINK}">@</text></g>')

def code_card(x, y, rot=8):
    return (f'<g transform="rotate({rot} {x + 20} {y + 15})">'
            f'<rect x="{x}" y="{y}" width="40" height="30" rx="5" fill="#15171B" stroke="{NAVY}" stroke-width="4"/>'
            f'<circle cx="{x + 7}" cy="{y + 7}" r="1.8" fill="#FF8FAE"/><circle cx="{x + 13}" cy="{y + 7}" r="1.8" fill="{m.YELLOW}"/><circle cx="{x + 19}" cy="{y + 7}" r="1.8" fill="#4CC38A"/>'
            f'<path d="M{x + 9} {y + 15} l5 4 -5 4" fill="none" stroke="#C9C6FF" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"/>'
            f'<path d="M{x + 18} {y + 23} h11" stroke="#C9C6FF" stroke-width="3" stroke-linecap="round"/></g>')

def approval_card(x, y, rot=-6):
    return (f'<g transform="rotate({rot} {x + 18} {y + 14})">'
            f'<rect x="{x}" y="{y}" width="38" height="28" rx="5" fill="#fff" stroke="{NAVY}" stroke-width="4"/>'
            f'<circle cx="{x + 11}" cy="{y + 14}" r="6.5" fill="#4CC38A"/>'
            f'<path d="M{x + 8} {y + 14} l2.4 2.6 4.4 -5" fill="none" stroke="#fff" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"/>'
            f'<path d="M{x + 21} {y + 10} h12 M{x + 21} {y + 18} h8" stroke="#B9BFEF" stroke-width="3" stroke-linecap="round"/></g>')

def key(x, y, rot=35):
    return (f'<g transform="rotate({rot} {x} {y})"><path d="M{x} {y} h22 M{x + 15} {y} v6 M{x + 21} {y} v5" fill="none" stroke="{NAVY}" stroke-width="9" stroke-linecap="round" stroke-linejoin="round"/>'
            f'<path d="M{x} {y} h22 M{x + 15} {y} v6 M{x + 21} {y} v5" fill="none" stroke="{m.YELLOW}" stroke-width="4" stroke-linecap="round" stroke-linejoin="round"/>'
            f'<circle cx="{x - 5}" cy="{y}" r="8" fill="{m.YELLOW}" stroke="{NAVY}" stroke-width="4"/><circle cx="{x - 5}" cy="{y}" r="2.6" fill="{NAVY}"/></g>')

def small_magnifier(x, y):
    return f'<g transform="translate({x} {y}) scale(.72)">{m.magnifier(0, 0)}</g>'

def screen_laptop():
    """A laptop seen from the front, its screen showing a few lines of code and a green check."""
    return (f'<rect x="62" y="122" width="76" height="44" rx="6" fill="#15171B" stroke="{NAVY}" stroke-width="4"/>'
            f'<path d="M71 133 h22" stroke="#FF8FAE" stroke-width="3.4" stroke-linecap="round"/>'
            f'<path d="M71 142 h38" stroke="#C9C6FF" stroke-width="3.4" stroke-linecap="round"/>'
            f'<path d="M71 151 h16" stroke="#8E88E0" stroke-width="3.4" stroke-linecap="round"/>'
            f'<circle cx="123" cy="148" r="8" fill="#4CC38A"/><path d="M119 148 l3 3 6 -7" fill="none" stroke="#fff" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"/>'
            f'<rect x="54" y="166" width="92" height="8" rx="4" fill="#B9BFEF" stroke="{NAVY}" stroke-width="4"/>')

# pose: (face, props). Props sit at the tips of the arms.
POSES = {
    "multitask": ("open",
        chat_bubble(4, 66) + approval_card(150, 70) + screen_laptop()),
    "investigate": ("open",
        chat_bubble(2, 64) + m.magnifier(150, 52)),
    "done": ("happy",
        m.sparkle(22, 40, 1.2, m.YELLOW) + m.sparkle(184, 70, 0.9, "#fff") + m.sparkle(186, 128, 0.8, m.YELLOW) + m.sparkle(14, 142, 0.8, "#fff") + m.check_badge(160, 24)),
    "sleep": ("closed",
        m.mug(150, 150) +
        f'<g fill="{NAVY}" font-family="ui-rounded, Arial Rounded MT Bold, system-ui, sans-serif" font-weight="800">'
        f'<text x="150" y="36" font-size="22">Z</text><text x="166" y="20" font-size="16">z</text><text x="178" y="8" font-size="11">z</text></g>'),
    "stop": ("serious", m.sign_stop(150, 40)),
    "oops": ("oops",
        m.sign_warning(150, 46) +
        f'<path d="M150 62 q6 12 0 16 q-6 -4 0 -16 Z" fill="#8ED4FF" stroke="{NAVY}" stroke-width="2.5" transform="translate(-86 -4)"/>'),
}

def svg(pose, dark=False):
    face, props = POSES[pose]
    out = (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" role="img" aria-label="Pouple the octopus">'
           f'{"" if dark else SHADOW}{LAYERS}{HEAD}{v.eyes(face)}{props}</svg>')
    if dark:
        # the navy outline vanishes on dark surfaces (contrast 1.2): strokes turn lavender, the face stays navy
        out = out.replace(f'stroke="{NAVY}"', f'stroke="{OUTLINE_ON_DARK}"').replace(f'<g fill="{NAVY}" font-family', f'<g fill="{LAVENDER}" font-family')
    return out

def icon():
    body = f'{LAYERS}{HEAD}{v.eyes("open")}'
    return ('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024" role="img" aria-label="Pouple">'
            '<defs><linearGradient id="g" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#DAD8FF"/><stop offset="1" stop-color="#A8A2FF"/></linearGradient></defs>'
            '<rect x="64" y="64" width="896" height="896" rx="200" fill="url(#g)"/>'
            f'<g transform="translate(76 76) scale(4.4)">{body}</g></svg>')

def favicon():
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="30 8 140 132"><path d="{fan.HEAD_PATH}" fill="{PINK}"/>'
            f'<path d="{fan.HEAD_OUTLINE}" fill="none" stroke="{NAVY}" stroke-width="5" stroke-linecap="round"/>{fan.HEAD_DETAILS}{v.eyes("open")}</svg>')

if __name__ == "__main__":
    out = pathlib.Path(__file__).parent
    for name in POSES:
        (out / f"mascot-{name}.svg").write_text(svg(name) + "\n")
        (out / f"mascot-{name}-dark.svg").write_text(svg(name, dark=True) + "\n")
    (out / "app-icon.svg").write_text(icon() + "\n")
    (out / "favicon.svg").write_text(favicon() + "\n")
    print("written:", ", ".join(POSES), "+ dark variants, app-icon, favicon")
