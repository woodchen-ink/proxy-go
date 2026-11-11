/**
 * 从 mark.json 生成全部图标 (单一来源 → 各尺寸)
 *
 *   npm i --no-save @resvg/resvg-js@2.6.2   (或装在别处, 用 NODE_PATH 指过去; 仓库不留 node_modules)
 *   node docs/design/brand/build-icons.js && python docs/design/brand/build-ico.py
 *
 * 产物:
 *   docs/design/brand/mark.svg / app-icon.svg / app-icon.png   标志与 1024 满幅应用图标
 *   web/app/icon.svg              256 圆角方块, 标志宽 90% (Next 自动挂 <link rel="icon">)
 *   web/app/apple-icon.png        180 满幅直角, 去 alpha 由 build-ico.py 完成
 *   web/components/brand-icon.tsx 侧栏 / 登录页内联用的圆角图标组件
 *   docs/design/brand/.ico/*.png  favicon 各档位图, 由 build-ico.py 打包成 web/app/favicon.ico
 */
const fs = require("fs")
const path = require("path")
const { Resvg } = require("@resvg/resvg-js")

const DIR = __dirname
const ROOT = path.resolve(DIR, "../../..")
const mark = JSON.parse(fs.readFileSync(path.join(DIR, "mark.json"), "utf8"))
const FROM = "#2EA7E0"
const TO = "#1D2088"

// icon 方块图标: 对角渐变底 + 白色标志; ratio 为标志宽占比, radius 为圆角比例
function icon(size, { ratio = 0.78, radius = 0.2237, id = "bg" } = {}) {
  const s = (size * ratio) / mark.w
  const tx = (size - mark.w * s) / 2
  const ty = (size - mark.h * s) / 2
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${size} ${size}" width="${size}" height="${size}"><defs><linearGradient id="${id}" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="${size}" y2="${size}"><stop offset="0" stop-color="${FROM}"/><stop offset="1" stop-color="${TO}"/></linearGradient></defs><rect width="${size}" height="${size}" rx="${(size * radius).toFixed(2)}" fill="url(#${id})"/><path transform="translate(${tx.toFixed(2)},${ty.toFixed(2)}) scale(${s.toFixed(5)})" fill="#fff" d="${mark.d}"/></svg>`
}

function png(svg, file) {
  fs.mkdirSync(path.dirname(file), { recursive: true })
  fs.writeFileSync(file, new Resvg(svg).render().asPng())
}

const markSvg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${mark.w} ${mark.h}"><defs><linearGradient id="g" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="${mark.w}" y2="0"><stop offset="0" stop-color="${FROM}"/><stop offset="1" stop-color="${TO}"/></linearGradient></defs><path fill="url(#g)" d="${mark.d}"/></svg>`
fs.writeFileSync(path.join(DIR, "mark.svg"), markSvg + "\n")

const app = icon(1024, { radius: 0 })
fs.writeFileSync(path.join(DIR, "app-icon.svg"), app + "\n")
png(app, path.join(DIR, "app-icon.png"))

fs.writeFileSync(path.join(ROOT, "web/app/icon.svg"), icon(256, { ratio: 0.9 }) + "\n")
png(icon(180, { radius: 0 }), path.join(ROOT, "web/app/apple-icon.png"))

for (const n of [16, 32]) png(icon(n, { ratio: 0.9 }), path.join(DIR, `.ico/${n}.png`))
png(icon(48), path.join(DIR, ".ico/48.png"))

// 内联组件: 渐变 id 用 useId 防同页多实例冲突
const s = 0.9 / mark.w * 32
const comp = `// 由 docs/design/brand/build-icons.js 生成, 不要手改; 改标志请改 mark.json 后重跑
import { useId } from "react"

const MARK = "${mark.d}"

// BrandIcon Proxy Go 应用图标 (圆角渐变方块 + 白色标志), 尺寸跟随 className
export function BrandIcon({ className }: { className?: string }) {
  const id = useId()
  return (
    <svg viewBox="0 0 32 32" className={className} aria-hidden>
      <defs>
        <linearGradient id={id} gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="32" y2="32">
          <stop offset="0" stopColor="${FROM}" />
          <stop offset="1" stopColor="${TO}" />
        </linearGradient>
      </defs>
      <rect width="32" height="32" rx="7.16" fill={\`url(#\${id})\`} />
      <path transform="translate(${((32 - mark.w * s) / 2).toFixed(3)},${((32 - mark.h * s) / 2).toFixed(3)}) scale(${s.toFixed(6)})" fill="#fff" d={MARK} />
    </svg>
  )
}
`
fs.writeFileSync(path.join(ROOT, "web/components/brand-icon.tsx"), comp)
console.log("icons written")
