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
