"""Puts each app screenshot beside its design reference, labelled, one image
per screen: <out>/<screen>.png = reference | app [| app dark].

    python3 scripts/ui/compare.py <shots dir> <out dir>
"""
import os
import sys

from PIL import Image, ImageDraw

REFS = os.path.join(os.path.dirname(__file__), "..", "..", "docs", "design", "goal-tree")
SCREENS = ["main", "subgoal", "proposal", "pivot", "finishline", "conversation", "record"]
W, H, BAND, GAP = 1280, 900, 36, 24


def panel(path, label):
    img = Image.open(path).convert("RGB").resize((W, H))
    out = Image.new("RGB", (W, H + BAND), "white")
    out.paste(img, (0, BAND))
    ImageDraw.Draw(out).text((8, 10), label, fill="black")
    return out


def main(shots, out):
    os.makedirs(out, exist_ok=True)
    for s in SCREENS:
        panels = [panel(os.path.join(REFS, s + ".png"), "design: " + s)]
        for name, label in ((s, "app"), (s + "-dark", "app, dark")):
            p = os.path.join(shots, name + ".png")
            if os.path.exists(p):
                panels.append(panel(p, label))
        sheet = Image.new("RGB", (len(panels) * W + (len(panels) - 1) * GAP, H + BAND), (200, 200, 200))
        for i, p in enumerate(panels):
            sheet.paste(p, (i * (W + GAP), 0))
        sheet.save(os.path.join(out, s + ".png"))
        print(os.path.join(out, s + ".png"))


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
