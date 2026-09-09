package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// Only the unchanged rule block shipped in v0.1.1 is eligible for migration.
const legacyDataGuardHash = "5866954a28a2abe8b2298bd30334bd4a6c33415d0dd1c38470b1d2cd2a61a184"

func upgradeDataGuard(prompt string) string {
	if strings.Count(prompt, DataGuardStart) != 1 || strings.Count(prompt, DataGuardEnd) != 1 {
		return prompt
	}
	start, end := strings.Index(prompt, DataGuardStart), strings.Index(prompt, DataGuardEnd)
	if end < start {
		return prompt
	}
	end += len(DataGuardEnd)
	block := strings.ReplaceAll(prompt[start:end], "\r\n", "\n")
	digest := sha256.Sum256([]byte(block))
	if hex.EncodeToString(digest[:]) != legacyDataGuardHash {
		return prompt
	}
	return prompt[:start] + DataGuardPrompt + prompt[end:]
}

func (e *Engine) upgradePrompt() error {
	s := e.Settings
	prompt := upgradeDataGuard(s.Prompt)
	if prompt == s.Prompt {
		return nil
	}
	// Preserve even an initial settings version that predates version snapshots.
	old, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if _, err = e.DB.Exec("INSERT INTO records(kind,id,payload) VALUES('settings_versions',?,?) ON CONFLICT(kind,id) DO NOTHING", fmt.Sprint(s.Version), old); err != nil {
		return err
	}
	s.Prompt = prompt
	s.Version++
	if err = e.change("settings", "current", s, "settings.prompt.upgrade", "更新未自定义的旧版 R7，保留其他提示词与开关状态"); err != nil {
		return err
	}
	e.Settings = s
	return nil
}
