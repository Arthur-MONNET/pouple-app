#!/usr/bin/env python3
"""Six proposals that keep the first Pouple (tall round head, thick navy outline, the multitask props) and only change
the tentacles: brand/proposals/<name>.svg. mascot_v1.py is the first version, kept untouched as the reference."""
import pathlib
import mascot_v1 as v

def lower(paths, width=15):
    return "".join(v.tube(d, width) for d in paths)

def render(paths, width=15, extra=""):
    arms, face, props = v.POSES["multitask"]
    # the first version's props include its own little suckers on a raised arm; keep them
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" role="img" aria-label="Pouple the octopus">'
            f'{v.SHADOW}{lower(paths, width)}{arms}{v.HEAD}{v.eyes(face)}{extra}{props}</svg>')

def render_bare(paths, width=15):
    """The octopus alone: no raised arms, no props."""
    # seen alone, the flat base of the head shows: round it so the arms leave a soft belly, not a shelf
    head = v.HEAD.replace("C146 130 130 138 L70 138 C54 130", "C146 130 130 138 C116 144 84 144 70 138 C54 130")
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" role="img" aria-label="Pouple the octopus">'
            f'{v.SHADOW}{lower(paths, width)}{head}{v.eyes("open")}</svg>')

def mirror(d):
    """Mirror a path around x = 100 (absolute coordinates only)."""
    out, nums = [], d.replace(",", " ").split()
    for token in nums:
        if token[0] in "MCLQ":
            out.append(token[0]); token = token[1:]
            if not token: continue
        try:
            out.append(token)
        except Exception:
            out.append(token)
    # a simple state machine: every pair is x y
    parts, buf = [], []
    for token in d.replace(",", " ").split():
        if token[0].isalpha():
            parts.append(token[0]); token = token[1:]
            if not token: continue
        buf.append(float(token))
        if len(buf) == 2:
            parts.append(f"{200 - buf[0]:g} {buf[1]:g}"); buf = []
    return " ".join(parts)

def pair(paths):
    return paths + [mirror(p) for p in paths]

# 1. the first version, as it was
ORIGINAL = [
    "M64 128 C46 146 28 150 30 168 C32 180 50 184 56 174",
    "M86 132 C82 154 72 168 80 180 C86 190 100 186 98 176",
    "M114 132 C118 154 128 168 120 180 C114 190 100 186 102 176",
    "M136 128 C154 146 172 150 170 168 C168 180 150 184 144 174",
]
# 2. the same four noodles, but they open outward and hook away from the centre
OUTWARD_4 = pair([
    "M64 128 C46 142 30 154 28 170 C27 181 16 182 12 174",
    "M84 134 C78 152 70 168 66 180 C63 189 52 189 50 182",
])
# 3. six arms fanning out (three a side)
OUTWARD_6 = pair([
    "M62 128 C42 134 28 148 30 162 C31 172 22 176 15 170",
    "M76 134 C64 148 62 164 68 176 C71 183 62 188 54 183",
    "M92 136 C88 150 90 164 96 174 C99 180 93 186 86 183",
])
# 4. eight thinner arms, four a side, the outer ones longest
EIGHT = pair([
    "M60 126 C40 130 24 142 22 158 C21 168 12 170 8 162",
    "M70 132 C52 142 42 158 44 172 C45 180 36 184 30 178",
    "M82 136 C72 148 68 164 72 176 C74 183 66 187 60 182",
    "M94 138 C90 150 92 164 96 174 C98 180 92 185 86 181",
])
# 5. a relaxed pose: the left side hooks outward, the right side waves
RELAXED = [
    "M64 128 C46 142 30 154 28 170 C27 181 16 182 12 174",
    "M84 134 C78 152 70 168 66 180 C63 189 52 189 50 182",
    "M116 134 C122 152 130 166 142 172 C152 176 160 170 158 162",
    "M138 126 C158 128 176 118 178 98 C179 90 188 88 190 94",
]
# 6. short and thick, splayed
THICK = pair([
    "M64 128 C48 138 36 150 38 164 C39 172 30 174 26 168",
    "M86 134 C80 148 78 162 84 172 C87 178 80 182 74 178",
])

PROPOSALS = {
    "1-original": (ORIGINAL, 15), "2-outward-4": (OUTWARD_4, 15), "3-outward-6": (OUTWARD_6, 14),
    "4-eight-arms": (EIGHT, 11), "5-relaxed": (RELAXED, 15), "6-short-thick": (THICK, 19),
}

if __name__ == "__main__":
    out = pathlib.Path(__file__).parent / "proposals"
    for name, (paths, width) in PROPOSALS.items():
        (out / f"{name}.svg").write_text(render(paths, width) + "\n")
        (out / f"bare-{name}.svg").write_text(render_bare(paths, width) + "\n")
    print("written:", ", ".join(PROPOSALS))
