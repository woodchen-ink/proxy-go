"""把 build-icons.js 输出的 16 / 32 (小尺寸符号) / 48 (透明底完整标志) 位图打包成 web/app/favicon.ico, 并给 apple-icon 去 alpha

    python docs/design/brand/build-ico.py
"""
from pathlib import Path

from PIL import Image

DIR = Path(__file__).resolve().parent
ROOT = DIR.parents[2]

frames = [Image.open(DIR / ".ico" / f"{n}.png").convert("RGBA") for n in (16, 32, 48)]
frames[2].save(ROOT / "web/app/favicon.ico", sizes=[(16, 16), (32, 32), (48, 48)], append_images=frames[:2])

# 满幅白底图标 iOS 自己裁圆角, 带 alpha 会出黑边
p = ROOT / "web/app/apple-icon.png"
Image.open(p).convert("RGB").save(p)
print("favicon.ico written")
