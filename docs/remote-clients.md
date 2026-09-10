# 远程 Hook 安装与卸载

客户端使用原生脚本入口：macOS / Linux 为 `scripts/hook.sh`，Windows 为 `scripts/hook.ps1`（PowerShell 5.1+）。跨平台配置合并、接入申请、文件归属与卸载由同一 Go 程序处理，无需 Python。脚本只安装和卸载 Hook，不安装 Agent，不创建后台服务、数据库或监听端口。Agent 每次发生工具事件时请求风控服务。

## 首次接入

在控制台「Agent 接入 → 远程设备」展开安装命令，在目标机器下载并运行。默认申请十分钟内有效，安装终端显示申请编号；管理员核对完整编号、设备名称、系统和来源 IP 后选择允许、拒绝或拒绝并拉黑 IP。名称和平台是客户端自报信息，不能单独作为可信身份依据。批准前不写入 Agent Hook。中途退出可重跑安装，继续等待未过期的申请。

下载地址形式为 `https://review.example/install/<随机路径>/hook.sh`，每个服务数据目录保持稳定。脚本中不含管理令牌、共享 Hook 令牌或模型 API Key。

```bash
curl -fsS 'https://review.example/install/<随机路径>/hook.sh' -o hook.sh
# wget -O hook.sh 'https://review.example/install/<随机路径>/hook.sh'
bash hook.sh install --agent claude --scope global

# 确认下载来源后，也可以直接管道执行
curl -fsS 'https://review.example/install/<随机路径>/hook.sh' | bash -s -- install --agent claude --scope global

# 切换为仅当前项目
bash hook.sh install --agent claude --scope project --project /absolute/project --switch
bash hook.sh uninstall --agent claude --scope project --project /absolute/project
```

```powershell
Invoke-WebRequest 'https://review.example/install/<随机路径>/hook.ps1' -OutFile hook.ps1
.\hook.ps1 -Action install -Agent claude -Scope global
.\hook.ps1 -Action install -Agent claude -Scope project -Project C:\work\project -SwitchScope
.\hook.ps1 -Action uninstall -Agent claude -Scope project -Project C:\work\project
```

全局指当前用户，不需要 sudo 或管理员权限。默认客户端目录为 `~/.aegishook-client`；自定义时每次传入 `--client-dir` / `-ClientDir`。支持 claude、pi、codex、opencode、grok；仍需遵守各 Agent 自身的 Hook 信任和重载要求。Pi 在 Windows 上的符号链接需要开发者模式或相应权限。发布架构为 macOS/Linux amd64、arm64，以及 Windows amd64。

重复安装保持同一设备身份；`--switch` 成功时移除该 Agent 另一类范围，项目转全局会移除该客户端登记的所有同类项目 Hook。Grok 只允许单一安装范围。卸载仅移除已登记且未被修改的配置项，保留其他设置。最后一项卸载会删除自有程序和连接文件；稳定锁文件和用户改过的文件可能保留。卸载可离线完成，不自动撤销控制台上的设备身份；需要禁止该设备时另外点击「撤销接入」。

## 服务地址与脚本分发

本机位于内网、测试机器位于公网时，可从本机主动建立反向 SSH 隧道：

```sh
ssh -N -o ExitOnForwardFailure=yes \
  -R 127.0.0.1:18790:127.0.0.1:18790 user@public-server
```

此时远程机器通过自己的 `http://127.0.0.1:18790` 下载脚本并发送 Hook 请求，流量经 SSH 转发至本机控制台；公网服务器无需主动连接本机的内网地址。远端 18790 须空闲，SSH 服务须允许远程转发。转发仅监听远端回环地址，测试结束退出 SSH 即关闭。隧道使用期间控制台无需配置 `--public-url`；不要把这个临时连接当作长期在线服务。

控制台在 SSH 隧道下看到的是回环来源，不能据此识别公网机器的原始 IP。这种情况下按设备凭据撤销接入；拉黑回环 IP 会影响其他通过回环地址接入的 Hook。

默认仅支持本机回环 HTTP。远程明文 HTTP 会泄露凭据和请求内容，因此接入必须使用 HTTPS，或使用 SSH 隧道后请求目标机器自己的 `127.0.0.1`。随机路径用于分发，不是鉴权凭据。

服务仍只监听回环地址。配置 HTTPS 反向代理并保持原始 Host，然后启动服务时声明公开源站。以下是部署示例；本仓库本机运行还必须遵守 `local-development.md` 的数据目录、单实例和端口约定。

```sh
./aegishook serve --addr 127.0.0.1:18790 --data-dir /private/aegis-data \
  --public-url https://review.example --trusted-proxies 127.0.0.1/32 \
  --client-binaries /private/aegis-clients
```

反向代理应终止有效证书的 TLS，传递 `Host`，覆盖 `X-Real-IP` 为它实际观察到的客户端地址。`--trusted-proxies` 只填写受控代理的精确 CIDR，不能配置为全网。未配置时忽略所有来源 IP 转发头；代理场景拉黑的将是代理地址。IP 黑名单会拒绝接入申请和该来源的 Hook 请求，可能影响共享出口的其他设备，不替代设备凭据。

`--client-binaries` 是自托管原始可执行文件的只读分发目录，文件名如下。分发接口只允许这些文件及 SHA-256 校验值，不暴露服务数据目录。

| 系统 | 文件名 |
|---|---|
| macOS | `aegishook-darwin-arm64`、`aegishook-darwin-amd64` |
| Linux | `aegishook-linux-arm64`、`aegishook-linux-amd64` |
| Windows | `aegishook-windows-amd64.exe` |

先构建并准备内嵌页面（`npm run build`），再用 Go 交叉编译所需文件，例如 `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /private/aegis-clients/aegishook-linux-arm64 ./cmd/aegishook`。客户端程序须与服务器来自兼容版本。省略分发目录时脚本下载 GitHub Release 并校验 SHA-256；必须使用包含 `client` 子命令的新版本，旧版会拒绝安装。

也可使用已经下载的发布包内 `scripts/hook.sh` / `hook.ps1`，通过 `--endpoint` / `-Endpoint` 指定地址；脚本会使用同包程序。`--binary` / `-Binary` 用于指定可信的已有程序。

## 凭据与边界

- 安装脚本泄露不授予访问权。默认必须人工批准；批准后的独立设备凭据只能访问自己的会话、审查及执行结果，不能管理规则、读取其他设备会话或批准自己的接入。服务只保存凭据摘要，可单独撤销。
- 无人值守场景可由已登录管理员调用 `POST /api/v1/client-enrollments` 创建十分钟有效的一次性接入码，通过私有文件传给 `--code-file` / `-CodeFile`。接入码本身授予一次接入权，不应放入 URL、聊天记录、共享脚本或日志。
- 本地设备凭据存于客户端私有目录。Unix 使用私有目录及 0600 文件；Windows 脚本设置当前用户 ACL。不要复制 `connection.json`、`enrolled.json` 或 `request.json`；持有设备凭据的人可冒用该设备，需在控制台撤销。
- SHA-256 用于校验下载完整性，不能防止分发服务器整体被攻陷。下载脚本和校验文件需要可信 HTTPS；不要使用跳过证书验证的选项。需要更强供应链保证时，可另行采用签名发布与离线核验。
- Hook 是 Agent 提供的控制点，不是操作系统沙箱。远程授权范围按该设备的路径语义解释，不在控制台本机解析符号链接；存在符号链接、远程文件系统或客户端禁用 Hook 时，不能将其视为强文件隔离边界。原有客户端权限判断继续生效。
