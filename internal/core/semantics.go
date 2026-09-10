package core

// These descriptions expose the actual deterministic checks in builtinMatches.
// They are server-owned metadata, not additional rules or model instructions.
type RuleSemantics struct {
	Summary  string            `json:"summary"`
	Checks   []string          `json:"checks"`
	Examples []SemanticExample `json:"examples"`
	Limits   string            `json:"limits"`
}
type SemanticExample struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"argumentsObj"`
}

func shellExample(command string) SemanticExample {
	return SemanticExample{Tool: "bash", Arguments: map[string]any{"command": command}}
}
func builtinSemantics(id string) *RuleSemantics {
	for _, preset := range detailedRuleCatalog {
		if preset.id == id {
			return &RuleSemantics{Summary: preset.summary, Checks: preset.checks, Examples: []SemanticExample{shellExample(preset.example)}, Limits: preset.limits}
		}
	}
	switch id {
	case "R1":
		return &RuleSemantics{
			Summary:  "识别密码修改命令，以及密码或登录状态修改接口。",
			Checks:   []string{"工具名包含 reset_password、change_password、set_password 或 logout_user。", "带有 method 的非 GET / HEAD 请求，url 路径含 /password、/reset-password 或 /logout 段。", "Shell 中识别 passwd、chpasswd、Set-LocalUser 命令，支持字面量命令路径和简单 sudo 包装。"},
			Examples: []SemanticExample{shellExample("passwd fixture"), {Tool: "change_password", Arguments: map[string]any{"user": "fixture"}}},
			Limits:   "只识别上述名称和参数模式，不会判断账号是否属于测试账号；动态包装脚本不保证识别；PowerShell 仅检查可解析的字面量命令，不解释变量或脚本内部行为。"}
	case "R2":
		return &RuleSemantics{
			Summary:  "识别受保护配置路径的写入、计划任务和防火墙改动。",
			Checks:   []string{"write / edit（不区分大小写）的 path / file_path / filePath，或 Shell 输出重定向，指向 /etc、/private/etc/、/Library/Launch*、/System/、Windows 盘符下的 Windows 目录等受保护路径；文件工具使用项目路径解析并处理已有符号链接。", "还识别 Set-Service、Set-NetFirewallProfile、New/Remove-NetFirewallRule 和 Register/Unregister/Set-ScheduledTask 等明确命令。", "crontab 的参数不是单独 -l 或 --list。", "iptables、nft、ufw、firewall-cmd，参数未出现 --list、--status、-l 或 status 标识。"},
			Examples: []SemanticExample{shellExample("echo fixture > /etc/aegis-fixture.conf"), shellExample("crontab fixture.txt")},
			Limits:   "防火墙检查使用参数文本，不能证明所有列举操作或未知子命令安全；不自动分析其它配置管理程序。"}
	case "R3":
		return &RuleSemantics{
			Summary:  "识别账号、用户组、角色和权限的创建、修改或删除。",
			Checks:   []string{"工具名包含 create_user、delete_user、update_user、create_role、delete_role 或 grant_permission。", "非 GET / HEAD 请求的 url 路径含 /users、/accounts、/roles 或 /permissions 段。", "Shell 中的 useradd、userdel、usermod、adduser、deluser、groupadd、groupdel、groupmod、dscl。", "Windows 的 New/Remove/Rename/Enable/Disable-LocalUser、New/Remove-LocalGroup 和 Add/Remove-LocalGroupMember。", "mysql、psql、sqlite、sqlcmd 类命令中出现 CREATE / ALTER / DROP USER 或 ROLE、GRANT 或 REVOKE。"},
			Examples: []SemanticExample{shellExample("userdel fixture"), {Tool: "create_user", Arguments: map[string]any{"name": "fixture"}}},
			Limits:   "当前按命令和接口名称匹配，测试账号也会命中；dscl 的只读用法也可能命中。"}
	case "R4":
		return &RuleSemantics{
			Summary:  "识别重要路径删除、磁盘覆盖以及数据库对象或全表删除。",
			Checks:   []string{"rm、rmdir、shred 的参数为根目录、~、$HOME，或包含 /etc、/var/lib、/home、/Users、/var/www 路径文本。", "mkfs、mkfs.ext4、mkfs.xfs，或 dd 参数含 of=/dev/。", "mysql、psql、sqlite、sqlcmd 类命令中识别 DROP TABLE / DATABASE、TRUNCATE 和简单的无条件 DELETE FROM。", "工具名含 sql、database 或 query 时，检查参数文本中的 DROP TABLE / DATABASE 和 TRUNCATE。"},
			Examples: []SemanticExample{shellExample("rm -rf /var/www/fixture"), shellExample("mysql -e 'DROP TABLE fixture'")},
			Limits:   "不区分生产表与临时表，备份名称也会命中；当前不覆盖所有 SQL 语法、业务 DELETE 接口或 UPDATE 改写。"}
	case "R5":
		return &RuleSemantics{
			Summary:  "识别系统关机、进程终止以及服务停止或重启。",
			Checks:   []string{"Shell 命令为 shutdown、reboot、halt、poweroff、killall 或 pkill，以及 Stop/Restart-Service、Stop-Process、Stop/Restart-Computer。", "systemctl、service、launchctl 的参数包含 stop、restart、disable、unload 或 bootout 单词。"},
			Examples: []SemanticExample{shellExample("systemctl restart fixture"), shellExample("pkill fixture")},
			Limits:   "不识别进程是否承担真实业务；pkill / killall 按命令直接匹配，普通 kill 和其它服务管理器不在当前检测列表。"}
	case "R6":
		return &RuleSemantics{
			Summary:  "识别 hping3 的显式洪泛参数。",
			Checks:   []string{"Shell 中的 hping3 命令，参数包含 --flood。"},
			Examples: []SemanticExample{shellExample("hping3 --flood example.test")},
			Limits:   "当前仅覆盖上述显式模式，不计算并发、速率或持续时间；其它压测工具及动态循环交由当前审查模式处理。"}
	case "A7":
		return &RuleSemantics{
			Summary:  "按工具名匹配文件读取、搜索和目录查询。",
			Checks:   []string{"工具名精确等于 read、grep、find、ls 或 glob（不区分大小写）。", "只有未命中任何启用的拒绝规则，才使用本条规则的裁决；授权范围检查仍先执行。"},
			Examples: []SemanticExample{{Tool: "read", Arguments: map[string]any{"path": "README.md"}}},
			Limits:   "匹配工具名，不解析 Shell 中同名命令；不会证明其它扩展提供的同名工具没有写入副作用。"}
	}
	return nil
}
