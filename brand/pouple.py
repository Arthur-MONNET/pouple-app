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

# pose: (face, props). Props sit at the tips of the arms.
POSES = {
    "multitask": ("open",
        m.phone(8, 56, -10) + m.envelope(158, 64, 10) + m.laptop() + m.pencil(22, 146, 38) + m.mug(150, 150)),
    "investigate": ("open",
        m.document(0, 100, -10) + m.magnifier(150, 52)),
    "done": ("happy",
        m.sparkle(20, 40, 1.2, m.YELLOW) + m.sparkle(182, 36, 1.2, "#fff") + m.sparkle(186, 130, 0.8, m.YELLOW) + m.sparkle(14, 140, 0.8, "#fff") + m.check_badge(100, 8)),
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
