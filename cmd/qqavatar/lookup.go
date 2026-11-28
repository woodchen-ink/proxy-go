package main

// lookup 按中控同样的读法查表 (两次区间读取 + 组内二分), 用于核对产物

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

func lookup(dir, hexHash string) (uint32, bool, error) {
	target, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(hexHash)))
	if err != nil {
		return 0, false, fmt.Errorf("哈希不是十六进制: %w", err)
	}
	kind := -1
	for k, size := range algoSizes {
		if len(target) == size {
			kind = k
		}
	}
	if kind < 0 {
		return 0, false, fmt.Errorf("哈希长度 %d 字节, 只支持 md5 (16) / sha256 (32)", len(target))
	}

	f, err := os.Open(partPath(dir, kind, int(target[0])))
	if err != nil {
		return 0, false, err
	}
	defer f.Close()

	bucket := int64(target[1])<<8 | int64(target[2])
	var head [8]byte
	if _, err := f.ReadAt(head[:], bucket*4); err != nil {
		return 0, false, err
	}
	start := binary.LittleEndian.Uint32(head[:4])
	end := binary.LittleEndian.Uint32(head[4:])
	if end <= start {
		return 0, false, nil
	}
	ids := make([]byte, (end-start)*4)
	if _, err := f.ReadAt(ids, headerBytes+int64(start)*4); err != nil && err != io.EOF {
		return 0, false, err
	}

	buf := make([]byte, 0, 24)
	lo, hi := 0, len(ids)/4-1
	for lo <= hi {
		mid := (lo + hi) / 2
		qq := binary.LittleEndian.Uint32(ids[mid*4:])
		h := digest(kind, email(buf, qq))
		switch c := bytes.Compare(h[:algoSizes[kind]], target); {
		case c == 0:
			return qq, true, nil
		case c < 0:
			lo = mid + 1
		default:
			hi = mid - 1
		}
	}
	return 0, false, nil
}
