// qqavatar 生成 "QQ 邮箱哈希 → QQ 号" 对照表, 供中控 /avatar 把 Gravatar 哈希还原成 QQ 头像
//
// 用法:
//
//	go run ./cmd/qqavatar build  -out D:\qqavatar [-min 10000] [-max 4294967295]
//	go run ./cmd/qqavatar lookup -out D:\qqavatar <md5 或 sha256 哈希>
//
// 产物 (表格式见 format.go) 用 scripts/qqavatar-upload.ps1 上传到 R2 桶 proxy-go-qqavatar
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "build":
		err = runBuild(os.Args[2:])
	case "lookup":
		err = runLookup(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: qqavatar build -out <目录> [-min N] [-max N] | qqavatar lookup -out <目录> <哈希>")
	os.Exit(2)
}

func runBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	out := fs.String("out", "", "输出目录")
	min := fs.Uint64("min", 10000, "最小 QQ 号")
	max := fs.Uint64("max", math.MaxUint32, "最大 QQ 号 (不超过 uint32)")
	_ = fs.Parse(args)
	if *out == "" || *min == 0 || *min > *max || *max > math.MaxUint32 {
		return fmt.Errorf("参数无效: -out 必填, 0 < min <= max <= %d", uint64(math.MaxUint32))
	}
	start := time.Now()
	if err := build(*out, uint32(*min), uint32(*max)); err != nil {
		return err
	}
	fmt.Printf("完成, 用时 %s\n", time.Since(start).Round(time.Second))
	return nil
}

func runLookup(args []string) error {
	fs := flag.NewFlagSet("lookup", flag.ExitOnError)
	out := fs.String("out", "", "表目录")
	_ = fs.Parse(args)
	if *out == "" || fs.NArg() != 1 {
		usage()
	}
	qq, ok, err := lookup(*out, fs.Arg(0))
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("未命中")
		return nil
	}
	fmt.Println(qq)
	return nil
}
