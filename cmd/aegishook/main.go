package main

import (
	"aegishook/internal/assets"
	"aegishook/internal/bridge"
	"aegishook/internal/core"
	"aegishook/internal/server"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Printf("AegisHook %s (Hook %s)\n", core.ReleaseVersion, core.Version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		f := flag.NewFlagSet("hook", flag.ContinueOnError)
		agent := f.String("agent", "", "客户端类型")
		connection := f.String("connection", "", "连接配置")
		if err := f.Parse(os.Args[2:]); err != nil {
			os.Exit(2)
		}
		os.Exit(bridge.Run(*agent, *connection, os.Stdin, os.Stdout, os.Stderr))
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	command := "serve"
	args := os.Args[1:]
	if len(args) > 0 && args[0][0] != '-' {
		command = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet("aegishook "+command, flag.ExitOnError)
	home, _ := os.UserHomeDir()
	data := flags.String("data-dir", filepath.Join(home, ".aegishook"), "私有数据目录")
	addr := flags.String("addr", "127.0.0.1:18790", "本机监听地址")
	agent := flags.String("agent", "pi", "pi、claude、codex、opencode、grok")
	scope := flags.String("scope", "global", "global 或 project")
	project := flags.String("project", "", "项目绝对路径")
	id := flags.String("id", "", "安装记录 ID")
	agentDir := flags.String("agent-dir", core.AgentDir(), "Pi 用户目录")
	flags.Parse(args)
	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("仅支持回环 IP 监听")
	}
	if command != "serve" {
		if ok, err := remoteCommand(command, *data, *addr, *scope, *project, *id, *agent); ok {
			return err
		}
	}
	e, err := core.Open(*data)
	if err != nil {
		return err
	}
	defer e.Close()
	admin, err := e.Secret("admin")
	if err != nil {
		return err
	}
	hook, err := e.Secret("hook")
	if err != nil {
		return err
	}
	code, err := assets.Files.ReadFile("adapter.ts")
	if err != nil {
		return err
	}
	if err = e.PrepareAdapter(code, "http://"+*addr, hook); err != nil {
		return err
	}
	plugin, err := assets.Files.ReadFile("opencode.mjs")
	if err != nil {
		return err
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	if err = e.PrepareIntegrations(plugin, binary); err != nil {
		return err
	}
	switch command {
	case "install":
		v, err := e.InstallAgent(*agent, *scope, *project, *agentDir)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(v)
	case "uninstall":
		if *id == "" {
			return fmt.Errorf("请用 --id 指定 status 中的安装记录")
		}
		return e.Uninstall(*id)
	case "status":
		is, err := e.Installations()
		if err != nil {
			return err
		}
		instances, err := e.Instances()
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"agents": core.DetectAgents(), "installations": is, "instances": instances})
	case "serve":
	default:
		return fmt.Errorf("支持 serve、install、uninstall、status")
	}
	ui, err := fs.Sub(assets.Files, "ui")
	if err != nil {
		return err
	}
	s := server.New(e, ui, admin, hook, *agentDir)
	httpServer := &http.Server{Addr: *addr, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	fmt.Printf("AegisHook %s: http://%s\n首次登录：使用 %s 中的管理令牌。\n", core.ReleaseVersion, *addr, filepath.Join(e.Dir, "admin.token"))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		httpServer.Shutdown(c)
	}()
	err = httpServer.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func remoteCommand(command, data, addr, scope, project, id, agent string) (bool, error) {
	token, err := os.ReadFile(filepath.Join(data, "admin.token"))
	if err != nil {
		return false, nil
	}
	client := &http.Client{Timeout: 5 * time.Second}
	b, _ := json.Marshal(map[string]string{"token": string(token)})
	res, err := client.Post("http://"+addr+"/api/v1/login", "application/json", bytes.NewReader(b))
	if err != nil {
		return false, nil
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return true, fmt.Errorf("服务管理认证失败")
	}
	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == "aegis_session" {
			cookie = c
		}
	}
	if cookie == nil {
		return true, fmt.Errorf("未收到管理会话")
	}
	endpoint := "/api/v1/installations"
	method := "GET"
	var body []byte
	switch command {
	case "install":
		method = "POST"
		body, _ = json.Marshal(map[string]string{"agent": agent, "scope": scope, "project": project})
	case "uninstall":
		if id == "" {
			return true, fmt.Errorf("请指定 --id")
		}
		method = "DELETE"
		endpoint += "/" + id
	case "status":
		values := map[string]any{}
		for _, kind := range []string{"agents", "instances", "installations"} {
			req, _ := http.NewRequest("GET", "http://"+addr+"/api/v1/"+kind, nil)
			req.AddCookie(cookie)
			res, err := client.Do(req)
			if err != nil {
				return true, err
			}
			if res.StatusCode != 200 {
				res.Body.Close()
				return true, fmt.Errorf("状态查询失败 HTTP %d", res.StatusCode)
			}
			var value any
			err = json.NewDecoder(res.Body).Decode(&value)
			res.Body.Close()
			if err != nil {
				return true, err
			}
			values[kind] = value
		}
		return true, json.NewEncoder(os.Stdout).Encode(values)
	default:
		return true, fmt.Errorf("未知命令")
	}
	req, _ := http.NewRequest(method, "http://"+addr+endpoint, bytes.NewReader(body))
	req.AddCookie(cookie)
	req.Header.Set("Content-Type", "application/json")
	out, err := client.Do(req)
	if err != nil {
		return true, err
	}
	defer out.Body.Close()
	if out.StatusCode >= 400 {
		return true, fmt.Errorf("管理操作失败 HTTP %d", out.StatusCode)
	}
	_, err = io.Copy(os.Stdout, out.Body)
	return true, err
}
