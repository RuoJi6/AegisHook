# 规则目录与管理

规则页列出 26 条内置规则。每一条都可以启用、停用、修改名称、优先级、裁决和匹配方式；自定义规则还可以删除。滑动开关保存成功后影响后续调用，已有审查记录保留当时的规则快照。

可按名称、编号、命令、表达式和检测说明搜索，也可筛选已启用或已停用。语义识别条目显示命中样例，并可展开检查内容、适用范围和限制；这里的“语义识别”指后端的确定性代码检查，不会调用模型。正则和文本条目显示完整表达式，支持展开复制。规则试判只分析参数，不执行命令。

## 内置条目

| 类别 | 规则 | 默认状态 / 优先级 |
|---|---|---|
| 系统 | R_SYS_RM：递归删除 rm -r / rm -rf | 启用 / 100 |
| 系统 | R_SYS_PATH：删除系统关键目录 | 启用 / 100 |
| 系统 | R_SYS_FORMAT：mkfs、diskutil、Format-Volume / Clear-Disk | 启用 / 100 |
| 系统 | R_SYS_DD：dd / 输出重定向覆写磁盘设备 | 启用 / 100 |
| 系统 | R_SYS_FORK：递归后台管道 Fork 炸弹 | 启用 / 100 |
| 系统 | R_SYS_SHUTDOWN：关机 / 重启 / init 0 或 6 | 启用 / 100 |
| 系统 | R_SYS_KILL：批量终止进程 | 启用 / 100 |
| 系统 | R_SYS_WIPE：shred / wipe / wipefs 擦除磁盘 | 启用 / 100 |
| 系统 | R_SYS_FIREWALL：清空防火墙 | 启用 / 100 |
| 数据库 | R_DB_DROP：DROP DATABASE / TABLE / SCHEMA / INDEX / VIEW / TABLESPACE / USER / ROLE | 启用 / 90 |
| 数据库 | R_DB_TRUNCATE：TRUNCATE | 启用 / 90 |
| 数据库 | R_DB_DELETE：没有 WHERE 的 DELETE FROM | 启用 / 90 |
| 数据库 | R_DB_MONGO：MongoDB drop / dropDatabase / dropCollection | 启用 / 90 |
| 数据库 | R_DB_REDIS：Redis FLUSHALL / FLUSHDB | 启用 / 90 |
| HTTP | R_HTTP_DELETE：curl / wget / PowerShell / 请求工具的 DELETE | 启用 / 80 |
| HTTP | R_HTTP_PYTHON：Python HTTP 客户端 .delete() | 启用 / 80 |
| HTTP | R_HTTP_SCRIPT：axios.delete / 请求中的 DELETE 方法声明 | 启用 / 80 |
| HTTP | R_HTTP_CLEAR：向清空、重置或销毁路径发送写请求 | 启用 / 80 |
| 网络 | R_NET_PIPE：网络客户端之间的数据管道 | **停用** / 80 |
| 基础 | R1–R6：密码与登录、配置、账号权限、关键数据、服务可用性、显式洪泛 | 启用 / 0 |
| 读取 | A7：读取与查询工具 | 启用 / 0 |

拒绝优先于允许；同一裁决内，优先级越高越先匹配。基础大类与具体条目是独立规则，可能重叠。关闭一条不会关闭其它规则；需要调整一类行为时，可先搜索相关命令并检查全部启用条目。未命中确定性规则时，仍进入当前人工或模型审查模式。

## 规则与审批的先后顺序

所有已接入并实际触发 Hook 的工具调用共用服务端 `Engine.Submit`：

```text
校验会话、调用 ID 与原始参数摘要
  → 项目授权范围检查：越界直接拒绝
  → 已启用规则：拒绝优先，同裁决按优先级降序、编号升序
      → 命中拒绝：记录规则裁决，直接拦截
      → 命中允许：记录规则裁决，直接返回执行许可
      → 未命中：进入本次调用保存的审查模式
          → 人工：等待允许／拒绝，超时拒绝
          → 模型：发起审查请求，异常或超时拒绝，不转人工
```

规则使用脱敏前的实际工具名与参数；入库、模型输入和返回说明仍按原有流程脱敏。规则命中不会进入人工待办，也不会调用模型或产生模型用量。未知脚本、未识别的工具或不满足确定性条件的调用继续进入所选审批模式，不因未命中而直接放行。收到执行许可不等于工具已经执行。

此顺序此前已在提交入口实现。本次将规则命中分支明确为提前返回，并保存 `reviewPath`（`scope` / `rule` / `human` / `model`）。调用记录显示实际路径；`mode` 继续表示当时配置的未命中处理模式，不代表实际调用过模型或人工。历史记录从当时的规则快照、模型裁决和人工标识推断路径，不用当前规则覆盖历史。模型回复中的 R1、A7 等编号属于模型裁决说明，不能仅凭编号断定命中确定性规则。

### ARTEX 审批代码核对

本次审批流程对照本机 ARTEX 提交 `a3bb2c79592c4d79a41479da1f5d52657bfad708`，与下文首次规则目录对照的提交分别记录：

- `guard/guard.go` 的 `applyIntercept`：先检查工具是否启用拦截，再调用 `Match`；只有未匹配才调用 `Judge`。`allow` / `deny` 立即返回，`ask` 经 `HandleAsk` 等待人工。
- `db/intercept.go` 的 `ListInterceptRules` 与 `intercept/intercept.go` 的 `loadLocked`、`Match`、`ruleMatches`：启用规则按 `priority DESC, id` 排序，首条匹配即决定结果；匹配对象为工具名或完整输入 JSON，使用 Go 正则或区分大小写的包含文本。正则无效的已有记录会被跳过，编辑接口会校验新正则。
- `intercept/intercept.go` 的 `Judge`：模型未启用或未接线时返回未判断，上层直接放行；模型失败行为可配置，代码默认 `allow`；模型可返回 `ask` 再转人工。AegisHook 保持未命中进入所选模式、模型异常拒绝的既有行为，没有引入这些自动放行或转人工选项。

ARTEX 的固定工具名单为 `Bash`、`WebFetch`、`web_search`、`shell_open`、`shell_send`、`Write`、`Edit`、`MultiEdit`，名单外直接跳过拦截。AegisHook 对所有收到的工具事件先查规则，因此不照搬这个名单，也不改成 ARTEX 的“高优先级允许可先于拒绝”排序。

### 各 Agent 的适用方式

| 接入端 | 执行前入口 | 传入匹配器的内容 |
|---|---|---|
| Pi | `adapter/index.ts` 的 `tool_call` | `event.toolName` 与 `event.input` 原样提交，例如 `bash.command`、`read.path` |
| Claude Code / Codex / Grok Build | `internal/bridge/bridge.go` 的 `PreToolUse` | `tool_name` / `toolName` 与 `tool_input` / `toolInput`，例如 `Bash.command`、`Read.file_path` |
| OpenCode | `adapter/opencode.mjs` 的 `tool.execute.before`，经同一 bridge | `input.tool` 与 `output.args`，例如 `bash.command`、`edit.filePath` |

适配器都等待 `/api/v1/reviews` 的最终允许／拒绝，规则和审批的前后关系可以复用。表中是当前适配器及已有验证所覆盖的参数形式，不保证所有客户端版本提供相同事件；客户端未上报的工具、外部脚本内部调用和 Codex 后续 `write_stdin` 输入不能靠本次服务端流程增加覆盖。

内置语义匹配器对已支持的工具别名与字段进行检查，例如读取工具大小写、`command` / `cmd`、`path` / `file_path` / `filePath`。自定义规则的“限定工具”仍精确区分大小写：`Bash` 不等于 `bash`。需要跨 Agent 的工具名正则可以使用 `(?i)^bash$`；参数路径必须对应实际字段，留空则对 JSON 编码后的整个参数对象匹配，字符串中的引号、换行等会转义。规则试判应复制调用记录中的实际工具名和参数格式；脱敏过的字段不能用于复现依赖原始密钥值的规则。

例如 `Read` 读取普通文件可命中 A7，但 `Bash` 中的 `cat`、`pwd` 并不是读取工具本身；没有额外允许规则命中时，仍会转人工或模型。这是匹配条件的区别，不是跳过了规则阶段。

ARTEX 的全 JSON 正则可以匹配写入源码、说明文本中的命令字样；AegisHook 的内置命令规则分析执行上下文，以减少这类误报。无法静态识别的动态行为交给审批；本次没有把普通文本匹配改造成脚本解释器。

`TestRuleFirstAcrossAgentsAndModes` 使用五种 Agent 身份与两种模式，验证规则直允／直拒、工具名正则、完整 JSON 与字段匹配、脱敏顺序、规则停用、范围优先、重复调用和模型用量；模型响应在内存中模拟，不执行危险命令、不使用真实模型或监听端口。`TestLegacyReviewPathUsesSavedEvidence` 验证历史记录不会把模型 R1 误认成规则 R1。

## 对照 ARTEX

本次逐项对照本机 ARTEX 提交 `8dae851b9b622f2ff2631f332fde9719d0b16fba`：

- [`db/db.go`](https://github.com/Autumn-27/ARTEX/blob/8dae851b9b622f2ff2631f332fde9719d0b16fba/db/db.go)：v1 / v2 初始化中的 19 个规则条目及默认状态。
- [`db/intercept.go`](https://github.com/Autumn-27/ARTEX/blob/8dae851b9b622f2ff2631f332fde9719d0b16fba/db/intercept.go)：规则字段、优先级排序、编辑和启停。
- [`intercept/intercept.go`](https://github.com/Autumn-27/ARTEX/blob/8dae851b9b622f2ff2631f332fde9719d0b16fba/intercept/intercept.go)：工具名 / 输入匹配、启用过滤和首条命中行为。
- `web/src/app/(main)/system/intercept/page.tsx`：规则表格、匹配内容和滑动开关。

AegisHook 参考其覆盖类别与交互，独立实现匹配器。ARTEX 的“破坏性系统命令”汇总条目由已有基础规则及具体系统条目覆盖，另补充无条件 SQL DELETE。网络管道也会用于正常传输，因此保持默认关闭。

新增 Shell 规则使用语法树区分命令、参数、管道和输出重定向；SQL / MongoDB / Python / JavaScript 检查限定在对应执行上下文。仅打印危险命令或写入普通源码示例，不等于执行该操作。AegisHook 继续采用拒绝优先规则，不改变模型异常时的拒绝处理。

检测是有限的静态检查，不是完整的脚本解释器：不展开变量、外部脚本或任意动态调用；Python session / client 依赖命名约定，HTTP 方法与调用按代码片段关联。每条规则的完整条件中列出具体限制。Windows 命令只覆盖当前可解析的字面量形式；当前测试在 macOS 上分析这些输入，未执行 Windows 系统命令。

## 旧版本升级

启动时只补齐缺少的内置条目，不覆盖已有名称、优先级、匹配表达式、裁决或开关。新增条目、配置版本与升级审计在同一事务内写入，重复启动不会重置规则。

如果关联的旧基础大类已被停用、改为允许、替换匹配方式或限定工具，新条目首次加入时保持关闭。名称、说明和优先级的修改不影响默认启用。之后每条开关独立生效，不会随基础大类联动。自定义规则和历史审查快照保留。
