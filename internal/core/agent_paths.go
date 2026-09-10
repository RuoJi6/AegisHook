package core

import (
	"path"
	"strings"
)

// Remote paths are interpreted on the reporting node, never resolved on the server disk.
func agentPathAbs(p, platform string) bool {
	if platform != "windows" {
		return strings.HasPrefix(p, "/")
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if len(p) >= 3 && ((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z')) && p[1] == ':' && p[2] == '/' {
		return true
	}
	if strings.HasPrefix(p, "//") {
		parts := strings.Split(strings.TrimPrefix(p, "//"), "/")
		return len(parts) >= 2 && parts[0] != "" && parts[0] != "?" && parts[1] != ""
	}
	return false
}
func agentPath(p, cwd, platform string) string {
	if p == "" {
		return ""
	}
	if platform == "windows" {
		p = strings.ReplaceAll(p, "\\", "/")
		cwd = strings.ReplaceAll(cwd, "\\", "/")
	}
	if !agentPathAbs(p, platform) {
		p = path.Join(cwd, p)
	}
	unc := platform == "windows" && strings.HasPrefix(p, "//")
	p = path.Clean(p)
	if unc {
		p = "/" + p
	}
	if platform == "windows" {
		p = strings.ToLower(p)
	}
	return p
}
func agentWithin(root, p, platform string) bool {
	root = agentPath(root, "", platform)
	p = agentPath(p, "", platform)
	return p == root || strings.HasPrefix(p, strings.TrimRight(root, "/")+"/")
}
