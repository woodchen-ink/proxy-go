package main

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestBuildAndLookup(t *testing.T) {
	out := t.TempDir()
	const min, max = 10000, 210000
	if err := build(out, min, max); err != nil {
		t.Fatal(err)
	}
	for _, qq := range []uint32{min, 12345, 99999, 123456, max} {
		e := email(nil, qq)
		m := md5.Sum(e)
		s := sha256.Sum256(e)
		for _, h := range []string{hex.EncodeToString(m[:]), hex.EncodeToString(s[:])} {
			got, ok, err := lookup(out, h)
			if err != nil || !ok || got != qq {
				t.Fatalf("lookup %d (%s) = %d, %v, %v", qq, h, got, ok, err)
			}
		}
	}
	// 号段外与非 QQ 邮箱都不应命中
	for _, addr := range []string{"9999@qq.com", "210001@qq.com", "someone@example.com"} {
		m := md5.Sum([]byte(addr))
		if _, ok, err := lookup(out, hex.EncodeToString(m[:])); err != nil || ok {
			t.Fatalf("%s 不应命中: ok=%v err=%v", addr, ok, err)
		}
	}
}
