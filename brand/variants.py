#!/usr/bin/env python3
"""Six renderings of Pouple, to compare styles (brand/variants/<name>.svg). They share the same character (round head,
six arms that fan outward and hook away from the centre); only the way it is drawn changes."""
import pathlib
import mascot as m

PINK, SHADE, LIGHT, NAVY, LAV = m.PINK, m.PINK_SHADE, m.PINK_LIGHT, m.NAVY, m.LAVENDER
HEAD = m.HEAD_PATH
LIMBS = m.LIMBS

def wrap(body, defs=""):
    return f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" role="img" aria-label="Pouple the octopus">{("<defs>" + defs + "</defs>") if defs else ""}{body}</svg>'

def sticker():
    return wrap(m.SHADOW + m.halo() + m.LOWER + m.HEAD + m.SUCKERS + f'<g transform="translate(0 -15)">{m.eyes("open")}</g>')

def flat():
    limbs = "".join(f'<path d="{d}" fill="none" stroke="{PINK}" stroke-width="17" stroke-linecap="round" stroke-linejoin="round"/>' for d in LIMBS)
    belly = "".join(f'<path d="{d}" fill="none" stroke="{SHADE}" stroke-width="4" stroke-linecap="round" stroke-dasharray="0.1 9" opacity=".9"/>' for d in LIMBS)
    head = (f'<path d="{HEAD}" fill="{PINK}"/><path d="M38 74 C38 100 56 126 100 127 C144 126 162 100 162 74 C150 96 126 108 100 108 C74 108 50 96 38 74 Z" fill="{SHADE}" opacity=".35"/>'
            f'<ellipse cx="66" cy="44" rx="15" ry="8" transform="rotate(-30 66 44)" fill="{LIGHT}" opacity=".9"/>')
    face = (f'<g transform="translate(0 -15)"><circle cx="74" cy="98" r="10" fill="{NAVY}"/><circle cx="126" cy="98" r="10" fill="{NAVY}"/>'
            f'<circle cx="77.5" cy="94.5" r="3.4" fill="#fff"/><circle cx="129.5" cy="94.5" r="3.4" fill="#fff"/>'
            f'<ellipse cx="54" cy="112" rx="8" ry="5" fill="{LIGHT}"/><ellipse cx="146" cy="112" rx="8" ry="5" fill="{LIGHT}"/>'
            f'<path d="M92 112 Q100 122 108 112" fill="none" stroke="{NAVY}" stroke-width="3.6" stroke-linecap="round"/></g>')
    return wrap(limbs + belly + head + face)

def line():
    c, w = "#F2668B", 5
    limbs = "".join(f'<path d="{d}" fill="none" stroke="{c}" stroke-width="{w + 2}" stroke-linecap="round" stroke-linejoin="round"/>' for d in LIMBS)
    head = f'<path d="{HEAD}" fill="#15171B" stroke="{c}" stroke-width="{w}" stroke-linejoin="round"/>'
    face = (f'<g transform="translate(0 -15)" fill="{c}"><circle cx="74" cy="98" r="7"/><circle cx="126" cy="98" r="7"/>'
            f'<path d="M91 111 Q100 121 109 111" fill="none" stroke="{c}" stroke-width="{w - 1}" stroke-linecap="round"/></g>'
            f'<path d="M132 30 C146 36 156 48 160 64" fill="none" stroke="{c}" stroke-width="3.5" stroke-linecap="round" opacity=".7"/>')
    return wrap('<rect width="200" height="200" fill="#15171B"/>' + limbs + head + face)

def soft3d():
    defs = (f'<radialGradient id="h" cx="35%" cy="25%" r="85%"><stop offset="0" stop-color="#FFB3C8"/><stop offset=".45" stop-color="{PINK}"/><stop offset="1" stop-color="#C73C68"/></radialGradient>'
            f'<linearGradient id="l" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="{PINK}"/><stop offset="1" stop-color="#C73C68"/></linearGradient>'
            f'<radialGradient id="e" cx="40%" cy="35%" r="70%"><stop offset="0" stop-color="#4A4590"/><stop offset="1" stop-color="{NAVY}"/></radialGradient>')
    shadow = '<ellipse cx="100" cy="184" rx="74" ry="7" fill="#000" opacity=".25"/>'
    limbs = "".join(f'<path d="{d}" fill="none" stroke="url(#l)" stroke-width="17" stroke-linecap="round" stroke-linejoin="round"/>'
                    f'<path d="{d}" fill="none" stroke="#fff" stroke-width="3" stroke-linecap="round" opacity=".28" transform="translate(-2.5 -2)"/>' for d in LIMBS)
    head = (f'<path d="{HEAD}" fill="url(#h)"/><ellipse cx="68" cy="40" rx="24" ry="11" transform="rotate(-28 68 40)" fill="#fff" opacity=".42"/>'
            f'<ellipse cx="62" cy="36" rx="9" ry="4" transform="rotate(-28 62 36)" fill="#fff" opacity=".7"/>')
    face = (f'<g transform="translate(0 -15)"><circle cx="74" cy="98" r="12" fill="url(#e)"/><circle cx="126" cy="98" r="12" fill="url(#e)"/>'
            f'<ellipse cx="78" cy="93" rx="4.4" ry="3.6" fill="#fff"/><ellipse cx="130" cy="93" rx="4.4" ry="3.6" fill="#fff"/>'
            f'<circle cx="71" cy="104" r="2" fill="#fff" opacity=".8"/><circle cx="123" cy="104" r="2" fill="#fff" opacity=".8"/>'
            f'<ellipse cx="52" cy="112" rx="9" ry="5.5" fill="#FF8FB0" opacity=".7"/><ellipse cx="148" cy="112" rx="9" ry="5.5" fill="#FF8FB0" opacity=".7"/>'
            f'<path d="M91 112 Q100 123 109 112" fill="none" stroke="#7A1F3F" stroke-width="3.4" stroke-linecap="round"/></g>')
    return wrap(shadow + limbs + head + face, defs)

def chibi():
    # an oversized head on four stubby arms: the head is scaled up, the arms are short curls
    arms = ["M60 118 C46 124 40 136 44 146 C46 152 38 156 32 150", "M82 126 C78 138 80 148 86 154 C89 158 83 164 77 160",
            "M140 118 C154 124 160 136 156 146 C154 152 162 156 168 150", "M118 126 C122 138 120 148 114 154 C111 158 117 164 123 160"]
    limbs = "".join(m.tube(d, 19) for d in arms)
    head = (f'<g transform="translate(-15 -10) scale(1.15)"><path d="{HEAD}" fill="{PINK}" stroke="{NAVY}" stroke-width="3.5" stroke-linejoin="round"/>'
            f'<path d="M132 30 C146 36 156 48 160 64" fill="none" stroke="{LIGHT}" stroke-width="6" stroke-linecap="round"/>'
            f'<ellipse cx="62" cy="48" rx="10" ry="6" transform="rotate(-35 62 48)" fill="{SHADE}"/><circle cx="80" cy="32" r="4" fill="{SHADE}"/>'
            f'<g transform="translate(0 -12)"><circle cx="72" cy="100" r="13" fill="{NAVY}"/><circle cx="128" cy="100" r="13" fill="{NAVY}"/>'
            f'<circle cx="77" cy="95" r="4.6" fill="#fff"/><circle cx="133" cy="95" r="4.6" fill="#fff"/><circle cx="68" cy="106" r="2.2" fill="#fff"/><circle cx="124" cy="106" r="2.2" fill="#fff"/>'
            f'<ellipse cx="50" cy="114" rx="9" ry="5.5" fill="{LIGHT}"/><ellipse cx="150" cy="114" rx="9" ry="5.5" fill="{LIGHT}"/>'
            f'<path d="M93 114 Q100 124 107 114 Z" fill="#B83A64" stroke="{NAVY}" stroke-width="3" stroke-linejoin="round"/></g></g>')
    return wrap('<ellipse cx="100" cy="184" rx="66" ry="6" fill="#231F52" opacity=".12"/>' + limbs + head)

def emblem():
    # a geometric badge: circle head, six rounded bars fanning out, two dots for eyes
    bg = f'<circle cx="100" cy="100" r="94" fill="{NAVY}"/><circle cx="100" cy="100" r="86" fill="none" stroke="{LAV}" stroke-width="2" opacity=".5"/>'
    bars = "".join(f'<rect x="-8" y="0" width="16" height="62" rx="8" fill="{PINK}" transform="translate(100 96) rotate({a})"/>' for a in (-62, -38, -14, 14, 38, 62))
    head = f'<circle cx="100" cy="82" r="46" fill="{PINK}"/><path d="M62 70 A40 40 0 0 1 100 44" fill="none" stroke="{LIGHT}" stroke-width="6" stroke-linecap="round"/>'
    face = f'<circle cx="84" cy="88" r="7" fill="{NAVY}"/><circle cx="116" cy="88" r="7" fill="{NAVY}"/><circle cx="86.5" cy="85.5" r="2.4" fill="#fff"/><circle cx="118.5" cy="85.5" r="2.4" fill="#fff"/><path d="M92 102 Q100 110 108 102" fill="none" stroke="{NAVY}" stroke-width="3.4" stroke-linecap="round"/>'
    return wrap(bg + bars + head + face)

VARIANTS = {"sticker": sticker, "flat": flat, "line": line, "soft3d": soft3d, "chibi": chibi, "emblem": emblem}

if __name__ == "__main__":
    out = pathlib.Path(__file__).parent / "variants"
    for name, fn in VARIANTS.items():
        (out / f"{name}.svg").write_text(fn() + "\n")
    print("written:", ", ".join(VARIANTS))
