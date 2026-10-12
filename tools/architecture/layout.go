package architecture

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	layoutTestSuffixes = []string{"_external_integration_test.go", "_integration_test.go", "_external_test.go", "_test.go"}
	layoutDocFile      = regexp.MustCompile(`[a-z0-9_]+\.go`)
)

// layoutViolation 记录一条文件组织规则及其定位。
type layoutViolation struct{ Rule, Path, Reason string }

// layoutFile 保存跨构建集合的语法树，生成文件也参与同名源文件匹配。
type layoutFile struct {
	path, name string
	tree       *ast.File
	generated  bool
}

// scanLayout 检查所有构建集合的文件名、标签和包说明。
func scanLayout(root string) ([]layoutViolation, error) {
	packages := map[string][]layoutFile{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "testdata" || entry.Name() == "vendor" || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		tree, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		packages[dir] = append(packages[dir], layoutFile{rel, entry.Name(), tree, ast.IsGenerated(tree)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	var violations []layoutViolation
	add := func(rule, path, reason string) { violations = append(violations, layoutViolation{rule, path, reason}) }
	for dir, files := range packages {
		names := map[string]bool{}
		count := 0
		packageName := ""
		var doc *layoutFile
		for i := range files {
			file := &files[i]
			names[file.name] = true
			if file.generated {
				continue
			}
			if file.name == "doc.go" {
				doc = file
			} else if !strings.HasSuffix(file.name, "_test.go") {
				count++
				packageName = file.tree.Name.Name
			}
		}
		for _, file := range files {
			if file.generated {
				continue
			}
			test := strings.HasSuffix(file.name, "_test.go")
			exempt := dir == "tests" || strings.HasPrefix(dir, "tests/") || dir == "migrations" || strings.HasPrefix(dir, "migrations/")
			if test && !exempt {
				if !layoutTestName(file.name, names) {
					add("test-name", file.path, "测试文件需要按被测源文件或场景命名")
				}
				expr, err := layoutBuildConstraint(file.tree)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", file.path, err)
				}
				eval := func(integration bool) bool {
					return layoutConstraintPossible(expr, map[string]bool{"integration": integration})
				}
				if strings.HasSuffix(file.name, "_integration_test.go") {
					if eval(false) || !eval(true) {
						add("test-tag", file.path, "集成测试需要 integration 构建约束")
					}
				} else if !eval(false) {
					add("test-tag", file.path, "单元测试需要在 integration=false 时参与构建")
				}
				if strings.Contains(file.name, "_external_") && !strings.HasSuffix(file.tree.Name.Name, "_test") {
					add("test-external", file.path, "external 测试文件需要使用外部测试包")
				}
				for _, decl := range file.tree.Decls {
					if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "TestMain" && file.name != "main_test.go" && file.name != "main_integration_test.go" {
						add("test-main", file.path, "TestMain 需要放在 main 测试文件")
					}
				}
			}
			stem := strings.TrimSuffix(file.name, ".go")
			if test {
				for _, suffix := range layoutTestSuffixes {
					if strings.HasSuffix(file.name, suffix) {
						stem = strings.TrimSuffix(file.name, suffix)
						break
					}
				}
			}
			parts := strings.Split(stem, "_")
			for k := 1; k < len(parts); k++ {
				if strings.Join(parts[:k], "") == filepath.Base(dir) {
					add("file-stutter", file.path, "文件名需要去掉重复的包名前缀")
					break
				}
			}
			if !test {
				switch file.name {
				case "helper.go", "helpers.go", "util.go", "utils.go", "common.go", "misc.go", "shared.go", "other.go":
					add("file-vague", file.path, "源文件名需要说明具体主题")
				}
			}
			if file.name != "doc.go" && file.tree.Doc != nil {
				add("package-comment", file.path, "包注释需要放在 doc.go")
			}
		}
		if count >= 15 && doc == nil {
			add("package-doc", filepath.ToSlash(filepath.Join(dir, "doc.go")), "手写源文件达到 15 个，需要 doc.go")
		}
		if doc != nil {
			valid := doc.tree.Doc != nil && len(doc.tree.Doc.List) > 0 && strings.HasPrefix(doc.tree.Doc.List[0].Text, "// Package "+doc.tree.Name.Name+" ") && len(doc.tree.Decls) == 0
			if packageName != "" && doc.tree.Name.Name != packageName {
				valid = false
			}
			if doc.tree.Doc != nil {
				for _, name := range layoutDocFile.FindAllString(doc.tree.Doc.Text(), -1) {
					if !names[name] {
						valid = false
					}
				}
			}
			if !valid {
				add("package-doc", doc.path, "doc.go 需要包注释、有效文件引用以及独立的 package 子句")
			}
		}
	}
	sort.Slice(violations, func(i, j int) bool {
		a, b := violations[i], violations[j]
		return a.Rule+"\t"+a.Path < b.Rule+"\t"+b.Path
	})
	return violations, nil
}

// layoutConstraintPossible 固定 integration 后查找可满足的构建标签组合。
// 平台与工具标签也可取 false，因此 !windows 等约束能参与检查。
func layoutConstraintPossible(expr constraint.Expr, values map[string]bool) bool {
	value, unknown := layoutConstraintValue(expr, values)
	if unknown == "" {
		return value
	}
	defer delete(values, unknown)
	values[unknown] = true
	if layoutConstraintPossible(expr, values) {
		return true
	}
	values[unknown] = false
	return layoutConstraintPossible(expr, values)
}

// layoutConstraintValue 返回已确定的结果，或一个还需赋值的标签。
func layoutConstraintValue(expr constraint.Expr, values map[string]bool) (bool, string) {
	switch node := expr.(type) {
	case nil:
		return true, ""
	case *constraint.TagExpr:
		value, ok := values[node.Tag]
		if !ok {
			return false, node.Tag
		}
		return value, ""
	case *constraint.NotExpr:
		value, unknown := layoutConstraintValue(node.X, values)
		return !value, unknown
	case *constraint.AndExpr:
		left, unknownLeft := layoutConstraintValue(node.X, values)
		if unknownLeft == "" && !left {
			return false, ""
		}
		right, unknownRight := layoutConstraintValue(node.Y, values)
		if unknownRight == "" && !right {
			return false, ""
		}
		if unknownLeft != "" {
			return false, unknownLeft
		}
		return left && right, unknownRight
	case *constraint.OrExpr:
		left, unknownLeft := layoutConstraintValue(node.X, values)
		if unknownLeft == "" && left {
			return true, ""
		}
		right, unknownRight := layoutConstraintValue(node.Y, values)
		if unknownRight == "" && right {
			return true, ""
		}
		if unknownLeft != "" {
			return false, unknownLeft
		}
		return left || right, unknownRight
	default:
		panic(fmt.Sprintf("未支持的构建约束类型：%T", expr))
	}
}

// layoutTestName 允许一个文件名按任一有效后缀匹配源文件。
func layoutTestName(name string, sources map[string]bool) bool {
	switch name {
	case "main_test.go", "main_integration_test.go", "helpers_test.go", "helpers_integration_test.go", "helpers_external_test.go", "helpers_external_integration_test.go":
		return true
	}
	if strings.HasSuffix(name, "_scenario_test.go") || strings.HasSuffix(name, "_scenario_integration_test.go") {
		return true
	}
	for _, suffix := range layoutTestSuffixes {
		if strings.HasSuffix(name, suffix) && sources[strings.TrimSuffix(name, suffix)+".go"] {
			return true
		}
	}
	return false
}

// layoutBuildConstraint 解析 package 子句之前的构建约束。
func layoutBuildConstraint(file *ast.File) (constraint.Expr, error) {
	for _, group := range file.Comments {
		if group.Pos() > file.Package {
			break
		}
		for _, comment := range group.List {
			if constraint.IsGoBuild(comment.Text) {
				return constraint.Parse(comment.Text)
			}
		}
	}
	return nil, nil
}
