// Command declorder 按 const、var、type、func 的顺序重排手写 Go 文件的顶层声明。
//
// 用法：
//
//	go run . [-w] <文件或目录>...
//
// 不带 -w 时列出需要调整的文件，有文件需要调整时退出码为 1。带 -w 时直接改写文件。
// 目录会递归处理，跳过 testdata、vendor、node_modules 和带生成标记的文件。
// 改写后的文件还需要运行 make fmt，由 gofumpt 和 gci 统一格式。
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	generatedHeader = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.\r?$`)
	packageLine     = regexp.MustCompile(`(?m)^package[ \t]`)

	// skippedDirs 里的目录不是包的手写源码。
	skippedDirs = map[string]bool{
		"testdata":     true,
		"vendor":       true,
		"node_modules": true,
	}
)

func main() {
	write := flag.Bool("w", false, "直接改写文件")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "用法：declorder [-w] <文件或目录>...")
		os.Exit(2)
	}

	files, err := collect(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	changed := 0
	failed := false
	for _, path := range files {
		ok, err := process(path, *write)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			failed = true
			continue
		}
		if ok {
			changed++
		}
	}

	switch {
	case failed:
		os.Exit(2)
	case changed > 0 && !*write:
		os.Exit(1)
	}
}

// process 处理一个文件，返回文件内容是否需要调整。
func process(path string, write bool) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	out, notes, err := reorder(src)
	if err != nil {
		return false, err
	}
	for _, note := range notes {
		fmt.Fprintf(os.Stderr, "%s: 待确认：%s\n", path, note)
	}
	if bytes.Equal(src, out) {
		return false, nil
	}
	fmt.Println(path)
	if !write {
		return true, nil
	}
	return true, os.WriteFile(path, out, 0o644)
}

// collect 展开参数里的目录，返回需要处理的手写 Go 文件。
func collect(args []string) ([]string, error) {
	var files []string
	for _, arg := range args {
		err := filepath.WalkDir(arg, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if path != arg && skippedDirs[entry.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || entry.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			generated, err := isGenerated(path)
			if err != nil || generated {
				return err
			}
			files = append(files, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

// isGenerated 按 package 声明前的 "// Code generated ... DO NOT EDIT." 识别生成文件。
func isGenerated(path string) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	header := packageLine.Split(string(src), 2)[0]
	return generatedHeader.MatchString(header), nil
}
