package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"
)

const (
	maxReviewScriptBytes = 1 << 20
	maxReviewScriptFiles = 8
)

var scriptInterpreter = regexp.MustCompile(`^(python[0-9.]*|bash|sh|zsh|fish|node|nodejs|ruby|perl|php|pwsh|powershell)$`)

type scriptReference struct {
	path, problem string
	direct        bool
}

func staticWord(w *syntax.Word) (string, bool) {
	if w == nil {
		return "", false
	}
	return staticParts(w.Parts)
}

func staticParts(parts []syntax.WordPart) (string, bool) {
	var b strings.Builder
	for _, part := range parts {
		switch p := part.(type) {
		case *syntax.Lit:
			if strings.ContainsAny(p.Value, "*?[]~\\") {
				return "", false
			}
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			if p.Dollar {
				return "", false
			}
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			s, ok := staticParts(p.Parts)
			if !ok {
				return "", false
			}
			b.WriteString(s)
		default:
			return "", false
		}
	}
	return b.String(), true
}

func scriptReferences(command, dir string, depth int) []scriptReference {
	if depth > 4 {
		return []scriptReference{{problem: "嵌套 Shell 命令超过静态解析范围"}}
	}
	tree, err := syntax.NewParser().Parse(strings.NewReader(command), "review")
	if err != nil {
		return []scriptReference{{problem: "无法静态解析当前 Shell 命令"}}
	}
	var refs []scriptReference
	add := func(word *syntax.Word, direct bool) {
		path, ok := staticWord(word)
		if !ok || path == "" {
			refs = append(refs, scriptReference{problem: "执行脚本路径包含动态展开，无法确定实际文件"})
			return
		}
		if !filepath.IsAbs(path) {
			if dir == "" {
				refs = append(refs, scriptReference{path: path, problem: "执行目录不确定，无法解析相对脚本路径"})
				return
			}
			path = filepath.Join(dir, path)
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			refs = append(refs, scriptReference{path: path, problem: "无法解析绝对路径"})
			return
		}
		refs = append(refs, scriptReference{path: absolute, direct: direct})
	}
	syntax.Walk(tree, func(node syntax.Node) bool {
		if _, ok := node.(*syntax.FuncDecl); ok {
			return false
		}
		stmt, ok := node.(*syntax.Stmt)
		if !ok {
			return true
		}
		call, ok := stmt.Cmd.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		args := call.Args
		name, ok := staticWord(args[0])
		if !ok {
			refs = append(refs, scriptReference{problem: "动态命令入口，无法确定是否执行脚本"})
			return true
		}
		for len(args) > 1 && (name == "env" || name == "sudo" || name == "exec" || name == "timeout" || name == "nohup") {
			args = args[1:]
			if name == "timeout" && len(args) > 1 {
				args = args[1:]
			}
			for len(args) > 1 {
				v, literal := staticWord(args[0])
				if !literal || (!strings.HasPrefix(v, "-") && !(name == "env" && strings.Contains(v, "="))) {
					break
				}
				args = args[1:]
			}
			name, ok = staticWord(args[0])
			if !ok {
				refs = append(refs, scriptReference{problem: "包装命令的执行入口不确定"})
				return true
			}
		}
		base := filepath.Base(name)
		if base == "cd" {
			if len(args) == 2 {
				path, known := staticWord(args[1])
				if known && path != "-" {
					if filepath.IsAbs(path) {
						dir = path
					} else if dir != "" {
						dir = filepath.Join(dir, path)
					}
				} else {
					dir = ""
				}
			} else {
				dir = ""
			}
			return true
		}
		if base == "source" || base == "." {
			if len(args) > 1 {
				add(args[1], false)
			} else {
				refs = append(refs, scriptReference{problem: "source 缺少脚本路径"})
			}
			return true
		}
		if scriptInterpreter.MatchString(base) {
			shell := base == "bash" || base == "sh" || base == "zsh" || base == "fish"
			for i := 1; i < len(args); i++ {
				value, literal := staticWord(args[i])
				if !literal {
					add(args[i], false)
					return true
				}
				if value == "-c" || value == "-e" || value == "--eval" || value == "--print" || (shell && strings.HasPrefix(value, "-") && strings.Contains(value, "c")) {
					if shell && i+1 < len(args) {
						inline, known := staticWord(args[i+1])
						if known {
							refs = append(refs, scriptReferences(inline, dir, depth+1)...)
						} else {
							refs = append(refs, scriptReference{problem: "Shell 内联执行内容不确定"})
						}
					}
					return true
				}
				if value == "-m" {
					refs = append(refs, scriptReference{problem: "模块执行未提供可确定的本地脚本文件"})
					return true
				}
				if value == "-" {
					break
				}
				if value == "--" {
					continue
				}
				if strings.HasPrefix(value, "-") {
					if value == "-W" || value == "-X" || value == "--require" || value == "-r" {
						i++
					}
					continue
				}
				add(args[i], false)
				return true
			}
			for _, redir := range stmt.Redirs {
				if redir.Op == syntax.RdrIn {
					add(redir.Word, false)
					return true
				}
				if redir.Hdoc != nil || redir.Op == syntax.WordHdoc {
					return true
				}
			}
			if len(args) > 1 || len(tree.Stmts) > 1 || strings.Contains(command, "|") {
				refs = append(refs, scriptReference{problem: "解释器从外部标准输入执行代码，无法取得文件快照"})
			}
			return true
		}
		if strings.Contains(name, "/") || knownScriptExtension(name) {
			add(args[0], true)
		}
		return true
	})
	return refs
}

func knownScriptExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sh", ".bash", ".zsh", ".fish", ".py", ".py3", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".rb", ".pl", ".php", ".ps1", ".lua":
		return true
	default:
		return false
	}
}

// CaptureScripts records only files statically referenced by a supported tool.
// It never executes a command or expands a path through a shell.
func CaptureScripts(workingDir, tool string, arguments map[string]any) ([]ReviewScript, string) {
	if !hasExact(tool, "Bash", "bash", "shell", "shell_open", "shell_send", "command", "script", "powershell", "PowerShell", "execute_command", "run_command", "terminal", "cmd") {
		return nil, "not_applicable"
	}
	key := "command"
	if tool == "shell_send" {
		key = "text"
	}
	command, _ := arguments[key].(string)
	if strings.TrimSpace(command) == "" {
		for _, candidate := range []string{"script", "code", "cmd"} {
			if value, ok := arguments[candidate].(string); ok {
				command = value
				break
			}
		}
	}
	if strings.TrimSpace(command) == "" {
		return nil, "not_applicable"
	}
	refs := scriptReferences(command, workingDir, 0)
	files := []ReviewScript{}
	seen := map[string]bool{}
	status := "complete"
	total := 0
	for _, ref := range refs {
		if seen[ref.path+ref.problem] {
			continue
		}
		seen[ref.path+ref.problem] = true
		file := ReviewScript{Path: ref.path, ReadError: ref.problem}
		if file.Path == "" {
			file.Path = "<无法确定的脚本>"
		}
		if ref.problem != "" {
			status = "incomplete"
			files = append(files, file)
			continue
		}
		if len(files) >= maxReviewScriptFiles {
			files = append(files, ReviewScript{Path: file.Path, ReadError: "本次调用超过 8 个脚本文件"})
			status = "incomplete"
			break
		}
		content, err := readScript(ref.path)
		if err != nil {
			file.ReadError = err.Error()
			if strings.Contains(file.ReadError, "超过") {
				file.Truncated = true
			}
			status = "incomplete"
			files = append(files, file)
			continue
		}
		file.Bytes = int64(len(content))
		if ref.direct && (bytes.HasPrefix(content, []byte{0x7f, 'E', 'L', 'F'}) || bytes.HasPrefix(content, []byte{'M', 'Z'})) {
			continue
		}
		if tool == "shell_send" {
			file.ReadError = "交互会话可能已切换目录或连接远端，无法确认本机文件就是实际执行脚本"
			status = "incomplete"
			files = append(files, file)
			continue
		}
		if !utf8.Valid(content) || bytes.ContainsRune(content, 0) {
			file.ReadError = "脚本不是有效 UTF-8 文本"
			status = "incomplete"
			files = append(files, file)
			continue
		}
		if len(content) > maxReviewScriptBytes || total+len(content) > maxReviewScriptBytes {
			file.ReadError = "脚本超过完整快照总量限制（1 MiB），未附加部分内容"
			file.Truncated = true
			status = "incomplete"
			files = append(files, file)
			continue
		}
		hash := sha256.Sum256(content)
		file.SHA256 = fmt.Sprintf("%x", hash)
		file.Content = string(content)
		file.Complete = true
		total += len(content)
		files = append(files, file)
	}
	if len(files) == 0 {
		return nil, "not_applicable"
	}
	return files, status
}

func readScript(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("不是普通文件")
	}
	if info.Size() > maxReviewScriptBytes {
		return nil, fmt.Errorf("脚本超过完整快照限制（1 MiB）")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxReviewScriptBytes+1))
}

func scriptDigest(status string, scripts []ReviewScript) string {
	b, _ := json.Marshal(struct {
		Status  string         `json:"status"`
		Scripts []ReviewScript `json:"scripts"`
	}{status, scripts})
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h)
}
