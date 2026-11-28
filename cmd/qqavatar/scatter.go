package main

// 第一趟: 遍历号段算两种哈希, 按哈希首字节把 QQ 号追加到 2×256 个溢写文件
// 全量 40 多亿条放不进内存, 先分区落盘, 第二趟再逐个分区排序

import (
	"bufio"
	"crypto/md5"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
)

const scatterChunk = 1 << 20

type spill struct {
	mu sync.Mutex
	f  *os.File
	w  *bufio.Writer
}

func spillPath(tmp string, kind, part int) string {
	return filepath.Join(tmp, fmt.Sprintf("%s-%02x.part", algoNames[kind], part))
}

func scatter(tmp string, min, max uint32) (err error) {
	var spills [len(algoNames)][partitions]*spill
	defer func() {
		for k := range spills {
			for _, s := range spills[k] {
				if s == nil {
					continue
				}
				if ferr := s.w.Flush(); ferr != nil && err == nil {
					err = ferr
				}
				if cerr := s.f.Close(); cerr != nil && err == nil {
					err = cerr
				}
			}
		}
	}()
	for k := range spills {
		for p := range partitions {
			f, err := os.Create(spillPath(tmp, k, p))
			if err != nil {
				return err
			}
			spills[k][p] = &spill{f: f, w: bufio.NewWriterSize(f, 256<<10)}
		}
	}

	total := uint64(max) - uint64(min) + 1
	chunks := (total + scatterChunk - 1) / scatterChunk
	var next, done atomic.Uint64
	var firstErr atomic.Value
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				c := next.Add(1) - 1
				if c >= chunks || firstErr.Load() != nil {
					return
				}
				lo := uint64(min) + c*scatterChunk
				hi := min64(lo+scatterChunk, uint64(max)+1)
				if err := scatterChunkRange(&spills, lo, hi); err != nil {
					firstErr.CompareAndSwap(nil, err)
					return
				}
				reportProgress("分区", done.Add(1), chunks)
			}
		}()
	}
	wg.Wait()
	if e := firstErr.Load(); e != nil {
		return e.(error)
	}
	return nil
}

// scatterChunkRange 算一段号的哈希, 先在本地按分区攒好再加锁整块写出, 减少锁竞争
func scatterChunkRange(spills *[len(algoNames)][partitions]*spill, lo, hi uint64) error {
	var local [len(algoNames)][partitions][]byte
	buf := make([]byte, 0, 24)
	for n := lo; n < hi; n++ {
		qq := uint32(n)
		msg := email(buf, qq)
		m := md5.Sum(msg)
		s := sha256.Sum256(msg)
		local[algoMD5][m[0]] = binary.LittleEndian.AppendUint32(local[algoMD5][m[0]], qq)
		local[algoSHA256][s[0]] = binary.LittleEndian.AppendUint32(local[algoSHA256][s[0]], qq)
	}
	for k := range local {
		for p, data := range local[k] {
			if len(data) == 0 {
				continue
			}
			sp := spills[k][p]
			sp.mu.Lock()
			_, err := sp.w.Write(data)
			sp.mu.Unlock()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

var progressMu sync.Mutex

// reportProgress 每过 5% 打一行进度
func reportProgress(stage string, done, total uint64) {
	step := max(total/20, 1)
	if done%step != 0 && done != total {
		return
	}
	progressMu.Lock()
	fmt.Printf("%s %d%%\n", stage, done*100/total)
	progressMu.Unlock()
}

func min64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
