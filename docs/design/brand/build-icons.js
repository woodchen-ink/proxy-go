/**
 * 从 mark.svg (完整标志) 与 glyph.svg (小尺寸符号) 生成全部图标
 *
 * 两个 SVG 拷贝自 czl-skill references/product-logo/marks|glyphs/proxy-go.svg, 是单一来源;
 * 改造型先改 czl-skill 再拷贝过来, 不在本仓库改。
 *
 *   npm i --no-save @resvg/resvg-js@2.6.2   (或装在别处, 用 NODE_PATH 指过去; 仓库不留 node_modules)
 *   node docs/design/brand/build-icons.js && python docs/design/brand/build-ico.py
 *
 * 产物:
 *   web/app/icon.svg                小尺寸符号 (Next 自动挂 <link rel="icon">, 浏览器标签亮暗通用)
 *   web/app/apple-icon.png          180 白色满幅底 + 渐变标志 (长边 70%), 去 alpha 由 build-ico.py 完成
 *   web/components/brand-icon.tsx   BrandLogo (侧栏页头 / 登录页, 暗色主题白色)
 *   docs/design/brand/.ico/*.png    favicon 各档位图 (16 符号, 32 / 48 透明底标志), 由 build-ico.py 打包
 *
 * 小尺寸符号只用于 16 / 20px 档 (icon.svg、favicon 16); 24px 及以上一律用完整标志
 */
const fs = require("fs")
const path = require("path")
const { Resvg } = require("@resvg/resvg-js")

const DIR = __dirname
const ROOT = path.resolve(DIR, "../../..")

// 读单 path SVG: viewBox 与 d
function load(file) {
  const src = fs.readFileSync(path.join(DIR, file), "utf8")
  const [x, y, w, h] = src.match(/viewBox="([^"]+)"/)[1].trim().split(/\s+/).map(Number)
  const d = src.match(/<path[^>]* d="([^"]+)"/)[1]
  return { src, x, y, w, h, d }
}
const mark = load("mark.svg")
const glyph = load("glyph.svg")
const FROM = "#2EA7E0"
const TO = "#246CC8"
const GRAD = `<linearGradient id="g" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="${FROM}"/><stop offset="1" stop-color="${TO}"/></linearGradient>`

// 方形画布里放标志: ratio 为长边占比, bg 为底色 (null = 透明)
function square(size, ratio, bg) {
  const s = (size * ratio) / Math.max(mark.w, mark.h)
  const tx = (size - mark.w * s) / 2 - mark.x * s
  const ty = (size - mark.h * s) / 2 - mark.y * s
  const rect = bg ? `<rect width="${size}" height="${size}" fill="${bg}"/>` : ""
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${size} ${size}" width="${size}" height="${size}"><defs>${GRAD}</defs>${rect}<path transform="translate(${tx.toFixed(3)},${ty.toFixed(3)}) scale(${s.toFixed(6)})" fill="url(#g)" d="${mark.d}"/></svg>`
}

// 小尺寸符号: 透明底, 撑满方形画布 (长边对齐, 短边居中)
function glyphSquare(size) {
  const m = Math.max(glyph.w, glyph.h)
  const vx = glyph.x - (m - glyph.w) / 2
  const vy = glyph.y - (m - glyph.h) / 2
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${vx.toFixed(2)} ${vy.toFixed(2)} ${m} ${m}" width="${size}" height="${size}"><path fill="${FROM}" d="${glyph.d}"/></svg>`
}

function png(svg, file) {
  fs.mkdirSync(path.dirname(file), { recursive: true })
  fs.writeFileSync(file, new Resvg(svg).render().asPng())
}

fs.writeFileSync(path.join(ROOT, "web/app/icon.svg"), glyphSquare(256) + "\n")
png(square(180, 0.7, "#fff"), path.join(ROOT, "web/app/apple-icon.png"))
png(glyphSquare(16), path.join(DIR, ".ico/16.png"))
for (const n of [32, 48]) png(square(n, 0.92, null), path.join(DIR, `.ico/${n}.png`))

// 内联组件: 渐变 id 用 useId 防同页多实例冲突; 暗色主题标志改纯白
const vb = (o) => `${o.x} ${o.y} ${o.w} ${o.h}`
const comp = `// 由 docs/design/brand/build-icons.js 生成, 不要手改; 改标志请更新 mark.svg / glyph.svg 后重跑
import { useId } from "react"

const MARK = "${mark.d}"

// BrandLogo Proxy Go 完整标志 (云朵括弧 + 双向箭头), 透明底, 亮色渐变 / 暗色白色; 24px 以上各处都用它
export function BrandLogo({ className }: { className?: string }) {
  const id = useId()
  return (
    <svg viewBox="${vb(mark)}" className={className} aria-hidden>
      <defs>
        <linearGradient id={id} x1="0" y1="0" x2="1" y2="0">
          <stop offset="0" stopColor="${FROM}" />
          <stop offset="1" stopColor="${TO}" />
        </linearGradient>
      </defs>
      <path fill={\`url(#\${id})\`} className="dark:fill-white" d={MARK} />
    </svg>
  )
}

`
fs.writeFileSync(path.join(ROOT, "web/components/brand-icon.tsx"), comp)
console.log("icons written")
