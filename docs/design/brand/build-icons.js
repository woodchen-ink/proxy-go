/**
 * 从 mark.json (完整标志) 与 glyph.json (小尺寸符号) 生成全部图标
 *
 *   npm i --no-save @resvg/resvg-js@2.6.2   (或装在别处, 用 NODE_PATH 指过去; 仓库不留 node_modules)
 *   node docs/design/brand/build-icons.js && python docs/design/brand/build-ico.py
 *
 * 产物:
 *   docs/design/brand/mark.svg          透明底标志, 横向渐变
 *   docs/design/brand/glyph.svg         透明底小尺寸符号, #2EA7E0 实色
 *   docs/design/brand/app-icon.svg/png  1024 满幅白底 + 渐变标志
 *   docs/design/brand/app-icon-512.png  512 白色圆角底 + 渐变标志 (样板库用)
 *   docs/design/brand/glyph-256.png     256 透明底小尺寸符号 (样板库用)
 *   web/app/icon.svg                    小尺寸符号 (Next 自动挂 <link rel="icon">, 浏览器标签亮暗通用)
 *   web/app/apple-icon.png              180 满幅白底 + 渐变标志, 去 alpha 由 build-ico.py 完成
 *   web/components/brand-icon.tsx       BrandIcon (登录页, 白色圆角底) / BrandGlyph (侧栏, 小尺寸符号)
 *   docs/design/brand/.ico/*.png        favicon 各档位图 (16 / 32 符号, 48 白底标志), 由 build-ico.py 打包
 */
const fs = require("fs")
const path = require("path")
const { Resvg } = require("@resvg/resvg-js")

const DIR = __dirname
const ROOT = path.resolve(DIR, "../../..")
const mark = JSON.parse(fs.readFileSync(path.join(DIR, "mark.json"), "utf8"))
const glyph = JSON.parse(fs.readFileSync(path.join(DIR, "glyph.json"), "utf8"))
const FROM = "#2EA7E0"
const TO = "#1D2088"
const BORDER = "#E5E7EB" // 白底在亮色背景上的极浅描边 (不加阴影的平台用)

// 应用图标: 白色底板 + 横向渐变标志; ratio 为标志宽占比, radius 为圆角比例 (0 = 满幅直角)
function icon(size, { ratio = 0.78, radius = 0.2237, border = false } = {}) {
  const s = (size * ratio) / mark.w
  const tx = (size - mark.w * s) / 2
  const ty = (size - mark.h * s) / 2
  const rx = (size * radius).toFixed(2)
  const sw = Math.max(1, size / 128)
  const stroke = border ? ` stroke="${BORDER}" stroke-width="${sw}"` : ""
  const inset = border ? sw / 2 : 0
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${size} ${size}" width="${size}" height="${size}"><defs><linearGradient id="g" gradientUnits="userSpaceOnUse" x1="${tx.toFixed(2)}" y1="0" x2="${(tx + mark.w * s).toFixed(2)}" y2="0"><stop offset="0" stop-color="${FROM}"/><stop offset="1" stop-color="${TO}"/></linearGradient></defs><rect x="${inset}" y="${inset}" width="${size - 2 * inset}" height="${size - 2 * inset}" rx="${rx}" fill="#fff"${stroke}/><path transform="translate(${tx.toFixed(2)},${ty.toFixed(2)}) scale(${s.toFixed(5)})" fill="url(#g)" d="${mark.d}"/></svg>`
}

// 小尺寸符号: 透明底, 撑满画布
function glyphSvg(size, color = FROM) {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${glyph.w} ${glyph.h}" width="${size}" height="${size}"><path fill="${color}" d="${glyph.d}"/></svg>`
}

function png(svg, file) {
  fs.mkdirSync(path.dirname(file), { recursive: true })
  fs.writeFileSync(file, new Resvg(svg).render().asPng())
}

const markSvg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${mark.w} ${mark.h}"><defs><linearGradient id="g" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="${mark.w}" y2="0"><stop offset="0" stop-color="${FROM}"/><stop offset="1" stop-color="${TO}"/></linearGradient></defs><path fill="url(#g)" d="${mark.d}"/></svg>`
fs.writeFileSync(path.join(DIR, "mark.svg"), markSvg + "\n")
fs.writeFileSync(path.join(DIR, "glyph.svg"), glyphSvg(glyph.w) + "\n")

const app = icon(1024, { radius: 0 })
fs.writeFileSync(path.join(DIR, "app-icon.svg"), app + "\n")
png(app, path.join(DIR, "app-icon.png"))
png(icon(512), path.join(DIR, "app-icon-512.png"))
png(glyphSvg(256), path.join(DIR, "glyph-256.png"))

fs.writeFileSync(path.join(ROOT, "web/app/icon.svg"), glyphSvg(glyph.w) + "\n")
png(icon(180, { radius: 0 }), path.join(ROOT, "web/app/apple-icon.png"))

for (const n of [16, 32]) png(glyphSvg(n), path.join(DIR, `.ico/${n}.png`))
png(icon(48, { border: true }), path.join(DIR, ".ico/48.png"))

// 内联组件: 渐变 id 用 useId 防同页多实例冲突
const V = 64
const s = (0.78 * V) / mark.w
const tx = (V - mark.w * s) / 2
const comp = `// 由 docs/design/brand/build-icons.js 生成, 不要手改; 改标志请改 mark.json / glyph.json 后重跑
import { useId } from "react"

const MARK = "${mark.d}"
const GLYPH = "${glyph.d}"

// BrandIcon Proxy Go 应用图标 (白色圆角底 + 渐变标志), 用于 ≥48px 的位置, 尺寸跟随 className
export function BrandIcon({ className }: { className?: string }) {
  const id = useId()
  return (
    <svg viewBox="0 0 ${V} ${V}" className={className} aria-hidden>
      <defs>
        <linearGradient id={id} gradientUnits="userSpaceOnUse" x1="${tx.toFixed(3)}" y1="0" x2="${(tx + mark.w * s).toFixed(3)}" y2="0">
          <stop offset="0" stopColor="${FROM}" />
          <stop offset="1" stopColor="${TO}" />
        </linearGradient>
      </defs>
      <rect x="0.5" y="0.5" width="${V - 1}" height="${V - 1}" rx="${(V * 0.2237).toFixed(2)}" fill="#fff" stroke="${BORDER}" />
      <path transform="translate(${tx.toFixed(3)},${((V - mark.h * s) / 2).toFixed(3)}) scale(${s.toFixed(6)})" fill={\`url(#\${id})\`} d={MARK} />
    </svg>
  )
}

// BrandGlyph Proxy Go 小尺寸符号 (透明底 #2EA7E0 双向箭头), 用于 ≤40px 的位置, 尺寸跟随 className
export function BrandGlyph({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 ${glyph.w} ${glyph.h}" className={className} aria-hidden>
      <path fill="${FROM}" d={GLYPH} />
    </svg>
  )
}
`
fs.writeFileSync(path.join(ROOT, "web/components/brand-icon.tsx"), comp)
console.log("icons written")
