package core

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Engine struct {
	IntegrationBinary string
	lock              *os.File
	DB                *sql.DB
	Dir               string
	mu                sync.Mutex
	Settings          Settings
	Notify            func()
	stop              chan struct{}
	clock             func() time.Time
}

func ID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func Open(dir string) (*Engine, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "process.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockProcessFile(lock); err != nil {
		lock.Close()
		return nil, errors.New("该数据目录已有 AegisHook 进程，请使用运行中服务的界面或命令")
	}
	opened := false
	defer func() {
		if !opened {
			lock.Close()
		}
	}()
	db, err := sql.Open("sqlite", filepath.Join(dir, "aegishook.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS records(kind TEXT NOT NULL,id TEXT NOT NULL,payload TEXT NOT NULL,PRIMARY KEY(kind,id));`); err != nil {
		db.Close()
		return nil, err
	}
	os.Chmod(filepath.Join(dir, "aegishook.db"), 0600)
	e := &Engine{DB: db, lock: lock, Dir: dir, stop: make(chan struct{}), clock: time.Now}
	e.Settings = Settings{Mode: "human", Model: ModelConfig{Protocol: "openai"}, Prompt: DefaultPrompt, Version: 1, ApprovalSeconds: 600, ModelSeconds: 20}
	if err = e.get("settings", "current", &e.Settings); err != nil && !errors.Is(err, sql.ErrNoRows) {
		db.Close()
		return nil, err
	}
	if e.Settings.Model.Protocol == "" {
		e.Settings.Model.Protocol = "openai"
	}
	key, err := os.ReadFile(filepath.Join(dir, "model.key"))
	if err == nil {
		e.Settings.Model.APIKey = string(key)
	} else if !os.IsNotExist(err) {
		db.Close()
		return nil, err
	}
	e.Settings.Model.HasKey = e.Settings.Model.APIKey != ""
	if err = e.upgradePrompt(); err != nil {
		db.Close()
		return nil, err
	}
	if err = e.put("settings", "current", e.Settings); err != nil {
		db.Close()
		return nil, err
	}
	rules, err := e.Rules()
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		for _, r := range BuiltinRules() {
			if err = e.put("rules", r.ID, r); err != nil {
				return nil, err
			}
		}
	} else if err = e.upgradeBuiltinRules(rules); err != nil {
		db.Close()
		return nil, err
	}
	reviews, err := e.Reviews()
	if err != nil {
		return nil, err
	}
	for _, r := range reviews {
		if r.Decision == "pending" {
			r.Decision = "reject"
			r.NeedsHuman = false
			r.RuleID = "E_RESTART"
			r.Comment = "审查服务已重启，原请求已失效，请重新提交。"
			r.Execution = "not_executed"
			now := e.clock()
			r.DecidedAt = &now
			if err = e.commitReview(r, "review.restart"); err != nil {
				return nil, err
			}
		}
	}
	instances, _ := e.Instances()
	for _, i := range instances {
		if i.State != "disconnected" {
			i.DisconnectReason = "service_restart"
		}
		i.State = "disconnected"
		i.Online = false
		e.put("instances", i.ID, i)
	}
	opened = true
	go e.sweepLoop()
	return e, nil
}
func (e *Engine) Close() error { close(e.stop); err := e.DB.Close(); e.lock.Close(); return err }
func (e *Engine) get(kind, id string, out any) error {
	var b []byte
	err := e.DB.QueryRow("SELECT payload FROM records WHERE kind=? AND id=?", kind, id).Scan(&b)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func (e *Engine) put(kind, id string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = e.DB.Exec("INSERT INTO records(kind,id,payload) VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET payload=excluded.payload", kind, id, b)
	return err
}
func list[T any](e *Engine, kind string) ([]T, error) {
	rows, err := e.DB.Query("SELECT payload FROM records WHERE kind=? ORDER BY json_extract(payload,'$.createdAt') DESC,id", kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var b []byte
		var v T
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (e *Engine) change(kind, id string, v any, action, detail string) error {
	tx, err := e.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if v == nil {
		_, err = tx.Exec("DELETE FROM records WHERE kind=? AND id=?", kind, id)
	} else {
		var b []byte
		b, err = json.Marshal(v)
		if err == nil {
			_, err = tx.Exec("INSERT INTO records(kind,id,payload) VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET payload=excluded.payload", kind, id, b)
		}
	}
	if err != nil {
		return err
	}
	var next *Settings
	if kind == "rules" || kind == "scopes" {
		s := e.Settings
		s.Version++
		next = &s
		b, marshalErr := json.Marshal(s)
		if marshalErr != nil {
			return marshalErr
		}
		if _, err = tx.Exec("INSERT INTO records(kind,id,payload) VALUES('settings','current',?) ON CONFLICT(kind,id) DO UPDATE SET payload=excluded.payload", b); err != nil {
			return err
		}
	}
	var versioned *Settings
	if kind == "settings" {
		if cfg, ok := v.(Settings); ok {
			versioned = &cfg
		}
	} else {
		versioned = next
	}
	if versioned != nil {
		snapshot, marshalErr := json.Marshal(versioned)
		if marshalErr != nil {
			return marshalErr
		}
		if _, err = tx.Exec("INSERT INTO records(kind,id,payload) VALUES('settings_versions',?,?) ON CONFLICT(kind,id) DO NOTHING", fmt.Sprint(versioned.Version), snapshot); err != nil {
			return err
		}
	}
	a := Audit{ID: ID(), Action: action, Subject: id, Detail: RedactText(detail), CreatedAt: e.clock()}
	b, _ := json.Marshal(a)
	if _, err = tx.Exec("INSERT INTO records(kind,id,payload) VALUES('audit',?,?)", a.ID, b); err != nil {
		return err
	}
	if err = tx.Commit(); err == nil {
		if next != nil {
			e.Settings = *next
		}
		if e.Notify != nil {
			e.Notify()
		}
	}
	return err
}
func (e *Engine) commitReview(r Review, action string) error {
	return e.change("reviews", r.ID, r, action, r.Comment)
}
func (e *Engine) Reviews() ([]Review, error) { return list[Review](e, "reviews") }
func (e *Engine) Rules() ([]Rule, error) {
	rules, err := list[Rule](e, "rules")
	for i := range rules {
		rules[i] = normalizeRule(rules[i])
	}
	sortRules(rules)
	return rules, err
}
func (e *Engine) Scopes() ([]Scope, error) { return list[Scope](e, "scopes") }
func (e *Engine) Audits() ([]Audit, error) { return list[Audit](e, "audit") }
func (e *Engine) Review(id string) (Review, error) {
	var r Review
	err := e.get("reviews", id, &r)
	return r, err
}
func (e *Engine) Instances() ([]Instance, error) {
	is, err := list[Instance](e, "instances")
	for j := range is {
		is[j].Online = is[j].State != "disconnected" && e.clock().Sub(is[j].Heartbeat) < 30*time.Second
	}
	return is, err
}
func (e *Engine) ConfigVersions() ([]Settings, error) { return list[Settings](e, "settings_versions") }
func (e *Engine) Config() Settings {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.Settings
	s.Model.APIKey = ""
	return s
}
func (e *Engine) SaveSettings(s Settings, key *string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s.Mode != "human" && s.Mode != "model" {
		return errors.New("无效审查模式")
	}
	if s.ApprovalSeconds < 10 || s.ApprovalSeconds > 86400 || s.ModelSeconds < 1 || s.ModelSeconds > 120 {
		return errors.New("超时设置超出范围")
	}
	if len(s.Prompt) < 20 || len(s.Prompt) > 32000 {
		return errors.New("提示词应为 20–32000 字符")
	}
	if err := s.Model.Pricing.validate(); err != nil {
		return err
	}
	if s.Model.Protocol == "" {
		s.Model.Protocol = "openai"
	}
	if !hasExact(s.Model.Protocol, "openai", "anthropic") {
		return errors.New("不支持的模型协议")
	}
	s.Model.APIKey = e.Settings.Model.APIKey
	changed := s.Model.Protocol != e.Settings.Model.Protocol || s.Model.BaseURL != e.Settings.Model.BaseURL || s.Model.Model != e.Settings.Model.Model
	if key != nil && *key != s.Model.APIKey {
		s.Model.APIKey = *key
		changed = true
	}
	s.Model.Tested = e.Settings.Model.Tested && !changed
	if s.Model.BaseURL != "" {
		if err := ValidateModelURL(s.Model.BaseURL); err != nil {
			return err
		}
	}
	if s.Mode == "model" && !s.Model.Tested {
		return errors.New("请先保存模型配置并测试成功，再启用模型审查")
	}
	s.Model.HasKey = s.Model.APIKey != ""
	s.Version = e.Settings.Version + 1
	if key != nil {
		if err := AtomicFile(filepath.Join(e.Dir, "model.key"), []byte(s.Model.APIKey), 0600); err != nil {
			return err
		}
	}
	if err := e.change("settings", "current", s, "settings.update", fmt.Sprintf("配置版本 %d，模式 %s", s.Version, s.Mode)); err != nil {
		if key != nil {
			if rollbackErr := AtomicFile(filepath.Join(e.Dir, "model.key"), []byte(e.Settings.Model.APIKey), 0600); rollbackErr != nil {
				return fmt.Errorf("配置保存失败且密钥回滚失败，请重新保存密钥")
			}
		}
		return err
	}
	e.Settings = s
	return nil
}
func AtomicFile(path string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".aegis-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
func (e *Engine) sweepLoop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-e.stop:
			return
		case <-t.C:
			e.Expire()
		}
	}
}
func (e *Engine) Expire() {
	e.mu.Lock()
	defer e.mu.Unlock()
	rs, err := e.Reviews()
	if err != nil {
		return
	}
	instances, err := e.Instances()
	if err != nil {
		return
	}
	online := map[string]bool{}
	for _, i := range instances {
		online[i.ID] = i.Online
	}
	for _, r := range rs {
		if r.Decision != "pending" {
			continue
		}
		action := ""
		if e.clock().After(r.Deadline) {
			r.RuleID = "E_TIMEOUT"
			r.Comment = "审查等待超时，本次调用已拒绝。"
			action = "review.timeout"
		} else if !online[r.InstanceID] {
			r.RuleID = "E_SESSION"
			r.Comment = "原会话已失联，本次审查已取消，请重新提交。"
			action = "review.cancel"
		}
		if action != "" {
			r.Decision = "reject"
			r.NeedsHuman = false
			r.Execution = "not_executed"
			now := e.clock()
			r.DecidedAt = &now
			e.commitReview(r, action)
		}
	}
}
