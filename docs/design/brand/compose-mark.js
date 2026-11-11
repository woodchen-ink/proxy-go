/**
 * 一次性脚本: 在 CZL 产品标志骨架上拼出 Proxy Go 的标志, 写 mark.json
 *
 * mark.json 才是单一来源, 本脚本只记录它是怎么来的; 调整符号时改这里重跑。
 * 骨架 (云朵 C 外框 + 三条速度线 + 底线) 取自 czl-code-skill 的 product-logo/skeleton.json,
 * 符号是"去程 + 回程"双向箭头 (反向代理 / 中转)。
 *
 *   node docs/design/brand/compose-mark.js <skeleton.json 路径>
 */
const fs = require("fs")
const path = require("path")

const skelPath = process.argv[2]
if (!skelPath) throw new Error("usage: node compose-mark.js <skeleton.json>")
const skel = JSON.parse(fs.readFileSync(skelPath, "utf8")) // 坐标系 813.5 × 438.1, 底线上沿 y≈373

const K = 0.35 // 前倾: 与速度线错位一致, x 随高度右移
const BASE = 373
const sk = ([x, y]) => [x + (BASE - y) * K, y]

// poly 实体统一顺时针, nonzero 填充下重叠部分自然合并
function poly(pts) {
  const area = pts.reduce((a, [x, y], i) => {
    const [x2, y2] = pts[(i + 1) % pts.length]
    return a + (x * y2 - x2 * y)
  }, 0)
  const p = (area > 0 ? pts : [...pts].reverse()).map(sk)
  return "M" + p.map(([x, y]) => `${x.toFixed(2)} ${y.toFixed(2)}`).join("L") + "Z"
}
const bar = (x0, x1, y, w) => poly([[x0, y - w / 2], [x1, y - w / 2], [x1, y + w / 2], [x0, y + w / 2]])
const head = (tipX, baseX, y, half) => poly([[baseX, y - half], [tipX, y], [baseX, y + half]])

const T = 56
const symbol = [
  bar(290, 500, 190, T), // 去程: 向右
  head(590, 494, 190, 62),
  bar(380, 600, 300, T), // 回程: 向左
  head(280, 386, 300, 62),
].join("")

const out = { d: skel.d + symbol, w: skel.w, h: skel.h }
fs.writeFileSync(path.join(__dirname, "mark.json"), JSON.stringify(out) + "\n")
console.log("wrote mark.json")
