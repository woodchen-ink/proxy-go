/**
 * 一次性脚本: 写小尺寸符号 glyph.json (≤32px 的 favicon / 侧栏收起图标用)
 *
 * glyph.json 才是单一来源, 本脚本只记录它是怎么来的; 调整符号时改这里重跑。
 * 取 mark.json 里的双向箭头, 去掉外框 / 速度线 / 底线, 放正 (不前倾, 16px 斜边锯齿明显),
 * 两支撑满 256 画布: 上支向右、下支向左, 杆粗 48 (16px 下 3 像素), 两支之间留 ≥ 2 像素缝。
 *
 *   node docs/design/brand/compose-glyph.js
 */
const fs = require("fs")
const path = require("path")

const S = 256
const T = 48 // 杆粗
const HALF = 54 // 箭头半高

const rect = (x0, x1, yc) => `M${x0} ${yc - T / 2}L${x1} ${yc - T / 2}L${x1} ${yc + T / 2}L${x0} ${yc + T / 2}Z`
const tri = (tip, base, yc) => `M${base} ${yc - HALF}L${tip} ${yc}L${base} ${yc + HALF}Z`

// 两支各自 bar + head 有重叠, nonzero 填充下自然合并 (矩形与三角形同为顺时针)
const d = [
  rect(8, 146, 66), // 上支: 去程, 向右
  tri(248, 140, 66),
  // 下支: 回程, 向左; 顶点按顺时针重排
  `M248 ${190 - T / 2}L248 ${190 + T / 2}L110 ${190 + T / 2}L110 ${190 - T / 2}Z`,
  `M116 ${190 - HALF}L116 ${190 + HALF}L8 190Z`,
].join("")

fs.writeFileSync(path.join(__dirname, "glyph.json"), JSON.stringify({ d, w: S, h: S }) + "\n")
console.log("wrote glyph.json")
