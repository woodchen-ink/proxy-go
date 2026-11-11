"""把 build-icons.js 输出的 16 / 32 / 48 位图打包成 web/app/favicon.ico, 并给满幅图标去 alpha

    python docs/design/brand/build-ico.py
"""
from pathlib import Path

from PIL import Image

DIR = Path(__file__).resolve().parent
ROOT = DIR.parents[2]

frames = [Image.open(DIR / ".ico" / f"{n}.png").convert("RGBA") for n in (16, 32, 48)]
frames[2].save(ROOT / "web/app/favicon.ico", sizes=[(16, 16), (32, 32), (48, 48)], append_images=frames[:2])

# 满幅直角图标 iOS 自己裁圆角, 带 alpha 会被拒收或出黑边
for p in (DIR / "app-icon.png", ROOT / "web/app/apple-icon.png"):
    Image.open(p).convert("RGB").save(p)
print("favicon.ico written")
