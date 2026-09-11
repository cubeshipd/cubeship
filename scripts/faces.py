#!/usr/bin/env python3
"""Draws the faces an account can wear: `scripts/faces.py <out dir>`.

Nine people, not one person in nine colours: the palette decides the
light in the room, and the character is a different character — a
runner, a diver, an operator, a rigger, an idol, an oracle, an enforcer
and two who wear nothing but their own head.

**This file is the source the PNGs are cut from, and that is why it is
here.** The originals of the set before this one were not kept — a
megabyte of PNG per face is a megabyte in every clone for ever — which
left nine images nobody could edit, only replace. A few hundred lines of
paths cost nothing to keep and can be changed a pixel at a time.

What makes these read at 24px as well as at 256 is value, not detail.
The head is a good deal lighter than the ground it sits on, the neon is
drawn twice — blurred underneath, crisp on top — and everything else is
kept off the face.

It writes **SVG only**. Turning that into PNG needs a real SVG engine —
rsvg-convert, Inkscape, or a browser — and ImageMagick's built-in one is
not one: it drops the filters the neon is made of and renders a flat
sticker with no sign that anything went wrong. Rasterise at 256, then
hand the directory to `scripts/profiles.sh`, which writes the master and
the thumbnail the sidebar loads on every screen.
"""
import pathlib
import sys

OUT = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else ".")
OUT.mkdir(parents=True, exist_ok=True)

# ground, key light, second light, face in shadow, face in the light
PALETTES = {
    "cyan":   ("#04070b", "#2de2e6", "#ff2e88", "#1b2630", "#42606e"),
    "blue":   ("#03060f", "#4d8dff", "#2de2e6", "#19202e", "#3a4c6e"),
    "hacker": ("#020602", "#3dff7a", "#8dff5a", "#16211a", "#31513c"),
    "mono":   ("#000000", "#ededed", "#8f8f8f", "#1d1d1d", "#4a4a4a"),
    "orange": ("#0a0603", "#ff9d2e", "#ffd166", "#241a12", "#5a4026"),
    "pink":   ("#0a0409", "#ff5cb8", "#ff2e88", "#241823", "#55344a"),
    "purple": ("#07050e", "#a97bff", "#2de2e6", "#1e1a2b", "#443a63"),
    "red":    ("#0a0405", "#ff4d5e", "#ffb020", "#241518", "#563037"),
    # The one face drawn on paper. Its "key light" is ink: on white
    # there is nothing for a neon to be brighter than, so the thing that
    # reads is the darkest mark rather than the lightest — and the
    # gradient runs the other way, the lit side of the head being the
    # pale one. See PAPER, which is what turns the grade off: the
    # vignette every other face is deepened by would only be grey smoke
    # across this one.
    "helix":  ("#ffffff", "#0a0a0a", "#595959", "#c4c4c4", "#fbfbfb"),
}

# Palettes whose ground is paper rather than night.
PAPER = {"helix"}

HEAD = ("M128 36 C97 36 80 56 78 86 C76 106 76 116 82 128 "
        "C88 152 106 172 128 172 C150 172 168 152 174 128 "
        "C180 116 180 106 178 86 C176 56 159 36 128 36 Z")

# The lit edge, as its own path so the rim light can be stroked on it
# alone. Tracing the whole head would put a line down the dark side too,
# which is the difference between a portrait and a sticker.
RIM = "M174 128 C180 116 180 106 178 86 C176 56 159 36 128 36"
RIM_FAR = "M82 128 C76 116 76 106 78 86 C80 56 97 36 128 36"


def svg(bg, key, dark, lit, body, paper=False):
    """The ground, the light in it, and the grade over everything."""
    return f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256" width="256" height="256">
<defs>
  <radialGradient id="halo" cx="50%" cy="58%" r="52%">
    <stop offset="0" stop-color="{key}" stop-opacity="{"0" if paper else ".38"}"/>
    <stop offset=".6" stop-color="{key}" stop-opacity="{"0" if paper else ".08"}"/>
    <stop offset="1" stop-color="{key}" stop-opacity="0"/>
  </radialGradient>
  <linearGradient id="skin" x1="1" y1="0" x2="0" y2=".4">
    <stop offset="0" stop-color="{lit}"/>
    <stop offset=".55" stop-color="{dark}"/>
  </linearGradient>
  <linearGradient id="shell" x1="1" y1="0" x2="0" y2=".4">
    <stop offset="0" stop-color="{lit}"/>
    <stop offset=".55" stop-color="#0d0607"/>
  </linearGradient>
  <linearGradient id="grade" x1="0" y1="0" x2="0" y2="1">
    <stop offset="0" stop-color="#000" stop-opacity="{".05" if paper else ".35"}"/>
    <stop offset=".45" stop-color="#000" stop-opacity="0"/>
    <stop offset="1" stop-color="#000" stop-opacity="{".08" if paper else ".5"}"/>
  </linearGradient>
  <pattern id="scan" width="3" height="3" patternUnits="userSpaceOnUse">
    <rect width="3" height="1" fill="#000" opacity="{".05" if paper else ".13"}"/>
  </pattern>
  <filter id="bloom" x="-60%" y="-60%" width="220%" height="220%">
    <feGaussianBlur stdDeviation="5"/>
  </filter>
  <filter id="soft" x="-60%" y="-60%" width="220%" height="220%">
    <feGaussianBlur stdDeviation="2"/>
  </filter>
  <clipPath id="face"><path d="{HEAD}"/></clipPath>
</defs>
<rect width="256" height="256" fill="{bg}"/>
<path d="M34 0V256M222 0V256" stroke="{key}" stroke-opacity=".06"/>
<rect width="256" height="256" fill="url(#halo)"/>
{body}
<rect width="256" height="256" fill="url(#scan)"/>
<rect width="256" height="256" fill="url(#grade)"/>
<rect x=".5" y=".5" width="255" height="255" fill="none"
      stroke="{key}" stroke-opacity=".4"/>
</svg>'''


def neon(shape, colour, blur="bloom", opacity=".85"):
    """Anything emissive: a blurred copy under a crisp one.

    Drawing the bright thing once gives a flat sticker. The light a neon
    source throws is most of what says it is a light, and it is the part
    that survives being shrunk to 24 pixels."""
    return (f'<g filter="url(#{blur})" opacity="{opacity}">{shape.format(c=colour)}</g>'
            f'{shape.format(c=colour)}')


def bust(bg, key, dark, cloth, edge=None):
    """Shoulders, neck and the light that catches one side of them."""
    edge = edge or key
    return f'''
<path d="M112 158 h32 v34 h-32 z" fill="{dark}" fill-opacity=".75"/>
<path d="M112 158 h32 v14 c-10 8 -22 8 -32 0 z" fill="#000" fill-opacity=".45"/>
<path d="M10 256 C16 212 52 190 98 182 L128 206 L158 182
         C204 190 240 212 246 256 Z" fill="{cloth}"/>
<path d="M158 182 C204 190 240 212 246 256" fill="none" stroke="{edge}"
      stroke-width="3" stroke-opacity=".6" stroke-linecap="round"/>
<path d="M10 256 C16 212 52 190 98 182" fill="none" stroke="{edge}"
      stroke-width="1.5" stroke-opacity=".18" stroke-linecap="round"/>
<path d="M98 182 L128 206 L158 182" fill="none" stroke="{edge}"
      stroke-width="2" stroke-opacity=".45"/>'''


def head(bg, key, dark, lit, rim=".95", far=".28", sheen=".10"):
    """Flat dark, a gradient toward the key, and a rim on the lit edge."""
    return f'''
<path d="{HEAD}" fill="url(#skin)"/>
<g clip-path="url(#face)">
  <path d="M128 30 C150 60 152 120 138 180 L200 180 L200 30 Z"
        fill="{key}" fill-opacity="{sheen}"/>
</g>
<path d="{RIM}" fill="none" stroke="{key}" stroke-width="3"
      stroke-opacity="{rim}" stroke-linecap="round"/>
<path d="{RIM_FAR}" fill="none" stroke="{key}" stroke-width="1.5"
      stroke-opacity="{far}" stroke-linecap="round"/>
<g clip-path="url(#face)" opacity=".55">
  <path d="M132 100 C130 116 126 126 120 132 C126 136 132 136 138 133"
        fill="none" stroke="#000" stroke-width="3" stroke-linecap="round"/>
</g>'''


def eyes(key, y=108, w=16, h=6, gap=20):
    x1, x2 = 128 - gap - w, 128 + gap
    return neon(f'<g fill="{{c}}"><rect x="{x1}" y="{y}" width="{w}" height="{h}"/>'
                f'<rect x="{x2}" y="{y}" width="{w}" height="{h}"/></g>',
                key, blur="soft", opacity=".9")


def mouth(key, y=146, w=20, o=".4"):
    return (f'<path d="M{128 - w // 2} {y} h{w}" stroke="{key}" stroke-width="2.5" '
            f'stroke-opacity="{o}" stroke-linecap="round"/>')


# --- the eight -------------------------------------------------------


def f_cyan(bg, key, k2, dark, lit):
    """Runner: a visor across the eyes, hair cropped and spiked."""
    return bust(bg, key, dark, "#0a0e14") + head(bg, key, dark, lit) + f'''
<path d="M76 84 C78 48 100 28 128 28 C156 28 178 48 180 84
         C172 60 152 52 128 52 C104 52 84 60 76 84 Z" fill="#06090d"/>
<path d="M92 32 L84 6 L106 26 L110 0 L126 24 L138 0 L146 26 L166 6 L160 34 Z"
      fill="#06090d"/>
<path d="M76 84 C78 48 100 28 128 28 C146 28 162 36 172 52
         C160 42 146 38 128 38 C104 38 86 56 76 84 Z"
      fill="{key}" fill-opacity=".35"/>
<rect x="70" y="96" width="116" height="22" fill="#03060a"/>
{neon('<g fill="{c}"><rect x="76" y="102" width="44" height="10"/>'
      '<rect x="136" y="102" width="44" height="10"/></g>', key, opacity=".7")}
<path d="M70 96 h116" stroke="{key}" stroke-width="2.5"/>
<path d="M70 118 h116" stroke="{key}" stroke-width="1" stroke-opacity=".4"/>
{mouth(key)}
{neon('<path d="M166 128 v16 h12" fill="none" stroke="{c}" stroke-width="2.5"/>',
      k2, blur="soft", opacity=".7")}'''


def f_blue(bg, key, k2, dark, lit):
    """Diver: hood up, two slits for eyes, a breather over the mouth."""
    return bust(bg, key, dark, "#070c17") + head(bg, key, dark, lit, rim=".5") + f'''
<path d="M56 168 C46 104 76 18 128 18 C180 18 210 104 200 168
         C194 122 190 74 170 54 C156 40 142 34 128 34
         C114 34 100 40 86 54 C66 74 62 122 56 168 Z" fill="#050914"/>
<path d="M128 18 C180 18 210 104 200 168 L186 168
         C192 112 180 46 146 30 Z" fill="{key}" fill-opacity=".55"/>
<path d="M56 168 C46 104 76 18 128 18 L128 34 C96 34 66 92 70 168 Z"
      fill="{key}" fill-opacity=".14"/>
{neon('<g fill="{c}"><path d="M86 104 l30 -6 v12 l-30 6 z"/>'
      '<path d="M170 104 l-30 -6 v12 l30 6 z"/></g>', key, blur="soft", opacity=".85")}
<path d="M100 130 h56 v18 c0 11 -12 20 -28 20 s-28 -9 -28 -20 z"
      fill="#070c17"/>
<path d="M100 130 h56 v18 c0 11 -12 20 -28 20 s-28 -9 -28 -20 z"
      fill="none" stroke="{key}" stroke-width="2.5" stroke-opacity=".9"/>
{neon('<path d="M110 142 h36 M114 153 h28" stroke="{c}" stroke-width="2.5" '
      'stroke-linecap="round"/>', k2, blur="soft", opacity=".6")}'''


def f_hacker(bg, key, k2, dark, lit):
    """Operator: a headset with a boom mic, hair scraped straight back."""
    return bust(bg, key, dark, "#050a05") + head(bg, key, dark, lit) + f'''
<path d="M78 88 C80 50 102 30 128 30 C154 30 176 50 178 88
         C170 66 152 56 128 56 C104 56 86 66 78 88 Z" fill="#040803"/>
<path d="M84 66 C98 46 114 40 128 40 C142 40 158 46 172 66
         C156 54 142 50 128 50 C114 50 100 54 84 66 Z"
      fill="{key}" fill-opacity=".3"/>
{eyes(key)}
{mouth(key)}
{neon('<path d="M66 114 C58 62 90 30 128 30 C166 30 198 62 190 114" '
      'fill="none" stroke="{c}" stroke-width="4.5" stroke-linecap="round"/>',
      key, opacity=".55")}
<rect x="56" y="104" width="20" height="34" rx="2" fill="#040803"
      stroke="{key}" stroke-width="2.5"/>
<rect x="180" y="104" width="20" height="34" rx="2" fill="#040803"
      stroke="{key}" stroke-width="2.5"/>
<path d="M76 134 C94 148 102 158 106 164" fill="none" stroke="{key}"
      stroke-width="2.5"/>
{neon('<circle cx="108" cy="166" r="5.5" fill="{c}"/>', k2, blur="soft")}'''


def f_mono(bg, key, k2, dark, lit):
    """Chrome: a bare head, one seam, and no colour in the room at all."""
    return bust(bg, key, dark, "#0c0c0c") + head(bg, key, dark, lit) + f'''
<path d="M128 36 C97 36 80 56 78 86 C84 62 102 48 128 48
         C154 48 172 62 178 86 C176 56 159 36 128 36 Z"
      fill="{key}" fill-opacity=".16"/>
<path d="M128 40 V168" stroke="{key}" stroke-width="1.5" stroke-opacity=".28"/>
<path d="M96 72 h22 M138 72 h22" stroke="{key}" stroke-width="2.5"
      stroke-opacity=".4" stroke-linecap="round"/>
{eyes(key, w=17, h=5)}
{mouth(key, o=".35")}
<g opacity=".75">
  <rect x="158" y="124" width="20" height="5" fill="{k2}"/>
  <rect x="158" y="133" width="12" height="5" fill="{k2}"/>
  <rect x="158" y="142" width="16" height="5" fill="{k2}"/>
</g>'''


def f_orange(bg, key, k2, dark, lit):
    """Rigger: welding goggles worn down, a work lamp on the shoulder."""
    return bust(bg, key, dark, "#130e07") + head(bg, key, dark, lit) + f'''
<path d="M78 90 C80 52 102 34 128 34 C154 34 176 52 178 90
         C174 68 168 58 128 58 C88 58 82 68 78 90 Z" fill="#0c0904"/>
<path d="M128 34 C154 34 176 52 178 90 L170 78 C164 56 150 48 128 48 Z"
      fill="{key}" fill-opacity=".55"/>
<path d="M60 104 h136" stroke="#0c0904" stroke-width="13"/>
<path d="M60 100 h20 v9 h-20 z M176 100 h20 v9 h-20 z" fill="{key}"
      fill-opacity=".55"/>
<circle cx="98" cy="106" r="19" fill="#0c0904" stroke="{key}" stroke-width="3.5"/>
<circle cx="158" cy="106" r="19" fill="#0c0904" stroke="{key}" stroke-width="3.5"/>
<path d="M117 106 h22" stroke="{key}" stroke-width="3.5"/>
{neon('<g fill="{c}"><circle cx="98" cy="106" r="9"/>'
      '<circle cx="158" cy="106" r="9"/></g>', k2, blur="soft", opacity=".75")}
{mouth(key, y=150, w=22)}
<path d="M104 160 C112 168 144 168 152 160" fill="none" stroke="{key}"
      stroke-width="1.5" stroke-opacity=".2"/>
{neon('<rect x="182" y="192" width="20" height="12" fill="{c}"/>', k2,
      blur="soft", opacity=".6")}'''


def f_pink(bg, key, k2, dark, lit):
    """Idol: one side shaved to the skin, the other a long fall of hair."""
    return bust(bg, key, dark, "#140b11") + head(bg, key, dark, lit) + f'''
<path d="M128 28 C160 28 184 50 184 92 L182 204 L156 204 L164 118
         C166 92 152 52 128 52 Z" fill="#0d0610"/>
<path d="M128 28 C156 28 178 46 183 80 L169 86 C163 64 148 52 128 52 Z"
      fill="{key}" fill-opacity=".6"/>
<path d="M184 92 L182 204 L162 204 L170 112 Z" fill="{key}" fill-opacity=".3"/>
<path d="M76 88 C78 54 98 28 128 28 L128 48 C104 48 84 62 80 92 Z"
      fill="#0d0610"/>
<path d="M82 60 h14 M78 74 h16 M76 88 h16" stroke="{key}" stroke-width="2.5"
      stroke-opacity=".45" stroke-linecap="round"/>
{eyes(key, y=110, w=15, h=5)}
{mouth(key, y=148, w=16)}
{neon('<circle cx="78" cy="120" r="6" fill="{c}"/>', k2, blur="soft")}
<path d="M78 126 C70 146 76 166 92 178" fill="none" stroke="{k2}"
      stroke-width="2.5" stroke-opacity=".8"/>'''


def f_purple(bg, key, k2, dark, lit):
    """Oracle: a third eye set in the forehead, hair veiling both sides."""
    return bust(bg, key, dark, "#0f0c19") + head(bg, key, dark, lit) + f'''
<path d="M128 26 C162 26 186 52 186 96 L180 210 L158 210 L168 118
         C170 82 152 52 128 52 C104 52 86 82 88 118 L98 210 L76 210
         L70 96 C70 52 94 26 128 26 Z" fill="#0a0713"/>
<path d="M128 26 C160 26 184 50 186 92 L172 96 C168 68 152 48 128 48 Z"
      fill="{key}" fill-opacity=".5"/>
<path d="M180 210 L158 210 L166 132 L181 132 Z" fill="{key}" fill-opacity=".25"/>
{eyes(key, y=112, w=15, h=5)}
{mouth(key, y=150, w=16)}
{neon('<g><path d="M128 66 l15 13 l-15 13 l-15 -13 z" fill="none" '
      'stroke="{c}" stroke-width="2.5"/><circle cx="128" cy="79" r="4.5" '
      'fill="{c}"/></g>', k2, blur="soft", opacity=".8")}'''


def f_red(bg, key, k2, dark, lit):
    """Enforcer: a closed helmet, one optic across it, an antenna."""
    return bust(bg, key, dark, "#150809") + f'''
<path d="{HEAD}" fill="{dark}"/>
<path d="M128 26 C162 26 182 52 182 92 C182 126 172 156 156 172
         L100 172 C84 156 74 126 74 92 C74 52 94 26 128 26 Z"
      fill="url(#shell)"/>
<path d="M128 26 C158 26 178 48 181 82 L168 88 C164 60 150 42 128 42 Z"
      fill="{key}" fill-opacity=".6"/>
<path d="M74 92 C74 52 94 26 128 26 L128 40 C102 40 88 60 88 92 Z"
      fill="{key}" fill-opacity=".16"/>
<rect x="80" y="98" width="96" height="20" fill="#060304"/>
{neon('<rect x="84" y="104" width="88" height="9" fill="{c}"/>', key, opacity=".7")}
<path d="M80 98 h96" stroke="{key}" stroke-width="2.5"/>
<path d="M98 136 h60 M104 149 h48 M112 161 h32" stroke="{key}"
      stroke-width="2.5" stroke-opacity=".3" stroke-linecap="round"/>
<path d="M170 40 L192 4" stroke="{key}" stroke-width="2.5"/>
{neon('<circle cx="193" cy="3" r="4.5" fill="{c}"/>', k2, blur="soft")}'''


def f_helix(bg, key, k2, dark, lit):
    """Fixer: an ocular plate over one eye, hair scraped into a knot.

    Drawn in ink rather than light, which is the whole of what this
    palette is. The marks that glow on the other eight are solid here —
    on paper the thing that carries is the darkest one, and a bloom
    under it reads as the ink bleeding rather than as a lamp.
    """
    return (bust(bg, key, dark, "#101010", edge="#ffffff")
            + head(bg, key, dark, lit, far=".5", sheen="0") + f'''
<path d="M78 88 C80 50 102 30 128 30 C154 30 176 50 178 88
         C170 64 152 54 128 54 C104 54 84 64 78 88 Z" fill="{key}"/>
<path d="M168 44 C186 40 196 48 194 62 C192 74 182 78 172 74
         C178 66 176 54 168 44 Z" fill="{key}"/>
<path d="M84 68 C98 50 112 44 128 44 C144 44 158 50 172 68
         C156 58 142 54 128 54 C114 54 100 58 84 68 Z"
      fill="#ffffff" fill-opacity=".14"/>
{neon('<rect x="92" y="106" width="16" height="6" fill="{c}"/>', key,
      blur="soft", opacity=".35")}
<rect x="136" y="94" width="46" height="30" fill="{key}"/>
<rect x="141" y="99" width="36" height="20" fill="#ffffff" fill-opacity=".12"/>
<path d="M146 109 h26" stroke="{bg}" stroke-width="3"/>
<path d="M136 124 L128 136 M182 94 l10 -10" stroke="{key}" stroke-width="3"
      stroke-linecap="round"/>
{mouth(key, y=150, w=18, o=".55")}
<path d="M98 168 C110 178 146 178 158 168" fill="none" stroke="{key}"
      stroke-width="2" stroke-opacity=".25"/>''')


FACES = {
    "cyan": f_cyan, "blue": f_blue, "hacker": f_hacker, "mono": f_mono,
    "orange": f_orange, "pink": f_pink, "purple": f_purple, "red": f_red,
    "helix": f_helix,
}

only = sys.argv[2:] or list(FACES)
for name in only:
    bg, key, k2, dark, lit = PALETTES[name]
    (OUT / f"{name}.svg").write_text(
        svg(bg, key, dark, lit, FACES[name](bg, key, k2, dark, lit),
            paper=name in PAPER))
print("drew:", ", ".join(only))
