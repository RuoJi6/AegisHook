package main

import (
	"aegishook/internal/core"
	"aegishook/internal/localaddr"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type options struct {
	command, dataDir, addr, agent, scope, project, id, agentDir string
	publicURL, clientBinaries, trustedProxy                     string
}

func parseOptions(args []string, output io.Writer) (options, error) {
	o := options{command: "serve"}
	if len(args) > 0 && args[0] == "help" {
		args = append(append([]string{}, args[1:]...), "-h")
	}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		o.command, args = args[0], args[1:]
	}
	switch o.command {
	case "serve", "install", "uninstall", "status":
	default:
		return o, fmt.Errorf("未知命令 %q；使用 aegishook -h 查看帮助", o.command)
	}
	f := flag.NewFlagSet("aegishook "+o.command, flag.ContinueOnError)
	f.SetOutput(output)
	home, _ := os.UserHomeDir()
	f.StringVar(&o.dataDir, "data-dir", filepath.Join(home, ".aegishook"), "私有数据目录")
	host := f.String("host", "127.0.0.1", "本机回环地址：localhost、127.x.x.x 或 ::1")
	port := f.Int("port", 18790, "监听端口（1–65535）")
	f.StringVar(&o.addr, "addr", "", "完整地址 host:port（IPv6 为 [::1]:port）；不能与 --host/--port 同用")
	if o.command == "serve" {
		f.StringVar(&o.clientBinaries, "client-binaries", "", "自托管客户端程序目录；省略时从 Release 下载")
		f.StringVar(&o.trustedProxy, "trusted-proxies", "", "允许设置 X-Real-IP 的代理 CIDR，逗号分隔；默认不信任")
		f.StringVar(&o.publicURL, "public-url", "", "HTTPS 反向代理的公开源站地址；默认只允许本机访问")
	}
	if o.command == "serve" || o.command == "install" {
		f.StringVar(&o.agentDir, "agent-dir", core.AgentDir(), "Pi 用户目录")
	} else {
		o.agentDir = core.AgentDir()
	}
	if o.command == "install" {
		f.StringVar(&o.agent, "agent", "pi", "客户端：pi、claude、codex、opencode、grok")
		f.StringVar(&o.scope, "scope", "global", "安装范围：global 或 project")
		f.StringVar(&o.project, "project", "", "项目绝对路径（--scope project 时使用）")
	}
	if o.command == "uninstall" {
		f.StringVar(&o.id, "id", "", "要卸载的安装记录 ID（通过 status 查看）")
	}
	f.Usage = func() {
		fmt.Fprintf(output, "AegisHook %s · 本机 Agent 执行审查控制台\n\n用法：\n  aegishook [命令] [选项]\n\n命令：\n  serve       启动控制台（默认）\n  install     安装 Agent Hook\n  uninstall   卸载指定 Hook\n  status      查看 Agent、安装记录与会话\n  hook        处理客户端 Hook 事件\n  version     查看版本（也支持 --version）\n\n%s 选项：\n", core.ReleaseVersion, o.command)
		f.PrintDefaults()
		fmt.Fprint(output, "  -h, --help\n        查看帮助；使用 aegishook <命令> -h 查看对应命令选项\n\n示例：\n  aegishook --host localhost --port 18800\n  aegishook serve --host ::1 --port 18800\n  aegishook serve --addr 127.0.0.1:18800\n  aegishook install --agent claude --host localhost --port 18800\n  aegishook status --host localhost --port 18800\n\n只允许本机回环地址。自定义地址或端口时，各命令需使用相同的连接参数和数据目录。\n")
	}
	if err := f.Parse(args); err != nil {
		return o, err
	}
	if f.NArg() != 0 {
		return o, fmt.Errorf("不支持的位置参数 %q；使用 aegishook %s -h 查看帮助", f.Args(), o.command)
	}
	seen := map[string]bool{}
	f.Visit(func(v *flag.Flag) { seen[v.Name] = true })
	if seen["addr"] {
		if seen["host"] || seen["port"] {
			return o, fmt.Errorf("--addr 不能与 --host 或 --port 同时使用")
		}
		var portText string
		var err error
		*host, portText, err = net.SplitHostPort(o.addr)
		if err != nil {
			return o, fmt.Errorf("--addr 格式应为 host:port（IPv6 使用 [::1]:port）：%w", err)
		}
		*port, err = strconv.Atoi(portText)
		if err != nil {
			return o, fmt.Errorf("端口必须是 1–65535 的整数")
		}
	}
	if !localaddr.IsLoopback(*host) {
		return o, fmt.Errorf("仅支持本机回环地址（localhost、127.x.x.x 或 ::1），收到 %q", *host)
	}
	if *port < 1 || *port > 65535 {
		return o, fmt.Errorf("端口必须在 1–65535 之间，收到 %d", *port)
	}
	o.addr = net.JoinHostPort(strings.ToLower(*host), strconv.Itoa(*port))
	if o.trustedProxy != "" {
		for _, v := range strings.Split(o.trustedProxy, ",") {
			if _, err := netip.ParsePrefix(strings.TrimSpace(v)); err != nil {
				return o, fmt.Errorf("--trusted-proxies 必须是逗号分隔的 IP CIDR")
			}
		}
	}
	if o.publicURL != "" {
		var err error
		o.publicURL, err = localaddr.Endpoint(o.publicURL)
		if err != nil || !strings.HasPrefix(o.publicURL, "https://") {
			return o, fmt.Errorf("--public-url 必须是 HTTPS 源站地址")
		}
	}
	return o, nil
}
