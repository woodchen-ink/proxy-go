package main

// 表格式 (中控 cloudflare-worker/src/public/avatar.ts 按同一格式读取, 两边改动要同步):
//
//	<algo>/<哈希首字节两位十六进制>.bin, algo 为 md5 / sha256, 每种 256 个文件
//	文件头: 65537 个 uint32 LE, 按哈希第 2~3 字节分组的累计条数 (第 i 组是 [head[i], head[i+1]))
//	正文:   uint32 LE 的 QQ 号, 按 "<QQ号>@qq.com" 的完整哈希升序排列
//
// 只存 QQ 号不存哈希: 查找时在组内二分, 每步现场计算候选号的哈希比较, 命中即已验证, 不会误判

import (
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strconv"
)

const (
	partitions     = 256
	bucketsPerPart = 1 << 16
	headerBytes    = (bucketsPerPart + 1) * 4
)

const (
	algoMD5 = iota
	algoSHA256
)

var algoNames = [...]string{algoMD5: "md5", algoSHA256: "sha256"}

var algoSizes = [...]int{algoMD5: md5.Size, algoSHA256: sha256.Size}

// email 把 QQ 号拼成 Gravatar 哈希的原文 (已是小写, 无需再规范化)
func email(buf []byte, qq uint32) []byte {
	buf = strconv.AppendUint(buf[:0], uint64(qq), 10)
	return append(buf, "@qq.com"...)
}

// digest 计算哈希, md5 只占前 16 字节, 其余为 0
func digest(kind int, msg []byte) (h [32]byte) {
	if kind == algoMD5 {
		s := md5.Sum(msg)
		copy(h[:], s[:])
		return h
	}
	return sha256.Sum256(msg)
}

func partPath(dir string, kind, part int) string {
	return filepath.Join(dir, algoNames[kind], fmt.Sprintf("%02x.bin", part))
}
