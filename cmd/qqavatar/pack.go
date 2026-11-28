package main

// 第二趟: 逐个分区读回 QQ 号, 重算哈希排序, 写出 "分组累计数 + 有序 QQ 号" 的表文件

import (
	"bufio"
	"cmp"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

type entry struct {
	k1, k2 uint64 // 哈希第 2~9 / 10~17 字节 (首字节同分区, 不必比较)
	qq     uint32
}

func build(out string, min, max uint32) error {
	tmp := filepath.Join(out, ".spill")
	for _, dir := range []string{tmp, filepath.Join(out, algoNames[algoMD5]), filepath.Join(out, algoNames[algoSHA256])} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := scatter(tmp, min, max); err != nil {
		return fmt.Errorf("分区: %w", err)
	}
	counts, err := packAll(tmp, out)
	if err != nil {
		return fmt.Errorf("排序写表: %w", err)
	}
	want := uint64(max) - uint64(min) + 1
	for k, c := range counts {
		if c != want {
			return fmt.Errorf("%s 条数 %d, 应为 %d", algoNames[k], c, want)
		}
	}
	if err := os.Remove(tmp); err != nil {
		return err
	}
	return writeManifest(out, min, max, want)
}

// packAll 并发处理 2×256 个分区; 每个分区约 1700 万条, 内存约 400MB
func packAll(tmp, out string) ([len(algoNames)]uint64, error) {
	var counts [len(algoNames)]atomic.Uint64
	total := uint64(len(algoNames) * partitions)
	var next, done atomic.Uint64
	var firstErr atomic.Value
	var wg sync.WaitGroup
	for range max(runtime.GOMAXPROCS(0)/2, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := next.Add(1) - 1
				if i >= total || firstErr.Load() != nil {
					return
				}
				kind, part := int(i/partitions), int(i%partitions)
				n, err := packPart(spillPath(tmp, kind, part), partPath(out, kind, part), kind, part)
				if err != nil {
					firstErr.CompareAndSwap(nil, fmt.Errorf("%s/%02x: %w", algoNames[kind], part, err))
					return
				}
				counts[kind].Add(n)
				reportProgress("写表", done.Add(1), total)
			}
		}()
	}
	wg.Wait()
	var result [len(algoNames)]uint64
	if e := firstErr.Load(); e != nil {
		return result, e.(error)
	}
	for k := range counts {
		result[k] = counts[k].Load()
	}
	return result, nil
}

func packPart(src, dst string, kind, part int) (uint64, error) {
	raw, err := os.ReadFile(src)
	if err != nil {
		return 0, err
	}
	es := make([]entry, len(raw)/4)
	buf := make([]byte, 0, 24)
	for i := range es {
		qq := binary.LittleEndian.Uint32(raw[i*4:])
		h := digest(kind, email(buf, qq))
		if int(h[0]) != part {
			return 0, fmt.Errorf("QQ 号 %d 不属于该分区, 溢写文件可能损坏", qq)
		}
		es[i] = entry{binary.BigEndian.Uint64(h[1:9]), binary.BigEndian.Uint64(h[9:17]), qq}
	}
	raw = nil
	slices.SortFunc(es, func(a, b entry) int {
		if c := cmp.Compare(a.k1, b.k1); c != 0 {
			return c
		}
		return cmp.Compare(a.k2, b.k2)
	})
	if err := writePart(dst, es); err != nil {
		return 0, err
	}
	return uint64(len(es)), os.Remove(src)
}

// writePart 先写临时文件再改名, 中断后不会留下半截的表文件
func writePart(dst string, es []entry) error {
	var head [bucketsPerPart + 1]uint32
	for _, e := range es {
		head[(e.k1>>48)+1]++
	}
	for i := 1; i < len(head); i++ {
		head[i] += head[i-1]
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	b := make([]byte, 0, 4)
	for _, v := range head {
		_, _ = w.Write(binary.LittleEndian.AppendUint32(b[:0], v))
	}
	for _, e := range es {
		_, _ = w.Write(binary.LittleEndian.AppendUint32(b[:0], e.qq))
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func writeManifest(out string, min, max uint32, count uint64) error {
	data, err := json.MarshalIndent(map[string]any{
		"min":                   min,
		"max":                   max,
		"count":                 count,
		"algos":                 algoNames,
		"partitions":            partitions,
		"buckets_per_partition": bucketsPerPart,
		"built_at":              time.Now().Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "manifest.json"), data, 0o644)
}
