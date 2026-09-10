#!/usr/bin/env bash
# Installs/uninstalls client hooks only; no daemon, database or listening port.
set -euo pipefail
umask 077
action=''; agent='claude'; scope='global'; project=''; endpoint="${AEGIS_INSTALL_ENDPOINT:-}"; code_file=''
client_dir="${HOME:?HOME is required}/.aegishook-client"
source_binary=''; version='latest'; switch_scope=false; tmp=''
download_base="${AEGIS_INSTALL_DOWNLOAD_BASE:-}"
die() { printf '%s\n' "$*" >&2; exit 1; }
usage() {
 cat <<'EOF'
用法：bash hook.sh install|uninstall [选项]
  --agent pi|claude|codex|opencode|grok  默认 claude
  --scope global|project              默认 global
  --project /absolute/project         项目范围的路径
  --endpoint https://review.example   风控服务地址（安装时必填）
  --code-file /private/enrollment-code  可选；省略时在控制台批准首次接入
  --client-dir /private/client        默认 ~/.aegishook-client
  --switch                            安装成功时移除同一 Agent 的另一类范围
  --binary /path/to/aegishook          使用已有程序（离线安装/源码测试）
  --version vX.Y.Z                     下载指定 Release；默认最新稳定版
  -h, --help                          查看帮助
没有参数时显示安装/卸载菜单。脚本不会安装 Agent 或启动风控服务。
EOF
}
if (( $#==0 )); then
 [[ -t 0 ]] || die '请指定 install 或 uninstall'
 printf '1. 安装 Hook\n2. 卸载 Hook\n'
 read -r -p '请选择 [1/2]：' choice
 case "$choice" in 1) action=install;;2) action=uninstall;;*) die '请选择 1 或 2';;esac
 read -r -p 'Agent [claude/pi/codex/opencode/grok，默认 claude]：' choice; agent="${choice:-claude}"
 read -r -p '范围 [global/project，默认 global]：' choice; scope="${choice:-global}"
else
 case "$1" in install|uninstall) action="$1";shift;;-h|--help) usage;exit 0;;*) die '只支持 install / uninstall';;esac
fi
while (( $# )); do
 case "$1" in
  --agent|--scope|--project|--endpoint|--code-file|--client-dir|--binary|--version)
   (( $#>=2 )) && [[ -n "$2" && "$2" != --* ]] || die "$1 缺少参数"
   case "$1" in
    --agent) agent="$2";;--scope) scope="$2";;--project) project="$2";;--endpoint) endpoint="$2";;
    --code-file) code_file="$2";;--client-dir) client_dir="$2";;--binary) source_binary="$2";;--version) version="$2";;
   esac;shift 2;;
  --switch) switch_scope=true;shift;;
  -h|--help) usage;exit 0;;
  *) die "未知选项：$1";;
 esac
done
case "$agent" in pi|claude|codex|opencode|grok) ;;*) die 'Agent 无效';;esac
case "$scope" in global|project) ;;*) die '范围须为 global 或 project';;esac
if [[ "$scope" == project && -z "$project" && -t 0 ]]; then read -r -p '项目绝对路径：' project;fi
[[ "$scope" != project || "$project" == /* ]] || die '项目须使用绝对路径'
[[ "$scope" != global || -z "$project" ]] || die 'global 范围不能指定项目'
[[ "$client_dir" == /* && "$client_dir" != / && ! "$client_dir" =~ [[:cntrl:]] ]] || die '客户端目录须为非根目录的绝对路径'
case "$(uname -s)" in Linux) os=linux;;Darwin) os=darwin;;*) die 'Windows 请使用 hook.ps1';;esac
case "$(uname -m)" in x86_64|amd64) arch=amd64;;arm64|aarch64) arch=arm64;;*) die '不支持该 CPU 架构';;esac
if [[ "$action" == uninstall && ( -n "$code_file" || "$switch_scope" == true ) ]]; then die '卸载不需要接入码或 --switch';fi
if [[ "$action" == install && -z "$endpoint" && -t 0 ]];then read -r -p '风控服务地址：' endpoint;fi
[[ "$action" != install || -n "$endpoint" ]] || die '安装时需要 --endpoint'
if command -v sha256sum >/dev/null 2>&1;then hash=(sha256sum);else hash=(shasum -a 256);fi
command -v "${hash[0]}" >/dev/null 2>&1 || die '需要 sha256sum 或 shasum'
binary="$client_dir/aegishook"
marker="$client_dir/binary.sha256"
cleanup(){ [[ -z "$tmp" ]] || rm -rf -- "$tmp"; }
trap cleanup EXIT
if [[ ! -e "$binary" ]]; then
 if [[ "$action" == uninstall && ! -e "$client_dir/client.json" ]];then printf '该范围没有已登记的 Hook。\n';exit 0;fi
 if [[ -z "$source_binary" && -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]];then
  script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
  if [[ -x "$script_dir/../aegishook" ]];then source_binary="$script_dir/../aegishook";fi
 fi
 tmp="$(mktemp -d "${TMPDIR:-/tmp}/aegishook-client.XXXXXXXX")"
 if [[ -z "$source_binary" ]];then
  [[ "$action" == install ]] || die 'Hook 程序缺失；请用 --binary 指定原版本程序恢复卸载能力'
  command -v curl >/dev/null 2>&1 || die '需要 curl'
  curl_args=(--fail --silent --show-error --location --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 300 --retry 2)
  if [[ -n "$download_base" ]];then
   if [[ "$download_base" =~ ^http://(127\.0\.0\.1|localhost|\[::1\])(:[0-9]+)?/ ]];then
    curl_args=(--fail --silent --show-error --proto '=http' --connect-timeout 15 --max-time 300 --retry 2)
   else
    [[ "$download_base" == https://* && "$download_base" != *'@'* ]] || die '程序下载需要 HTTPS 或回环 HTTP'
    curl_args=(--fail --silent --show-error --proto '=https' --connect-timeout 15 --max-time 300 --retry 2)
   fi
   curl "${curl_args[@]}" -o "$tmp/aegishook" "$download_base/bin/$os/$arch"
   expected="$(curl "${curl_args[@]}" "$download_base/sha256/$os/$arch")"
   actual="$("${hash[@]}" "$tmp/aegishook" | awk '{print $1}')"
   [[ "$expected" =~ ^[0-9a-f]{64}$ && "$expected" == "$actual" ]] || die '客户端程序 SHA-256 校验失败'
  else
  release='https://github.com/RuoJi6/AegisHook/releases'
  if [[ "$version" == latest ]];then
   resolved="$(curl "${curl_args[@]}" -o /dev/null -w '%{url_effective}' "$release/latest")"
   [[ "$resolved" == "$release/tag/"* ]] || die '无法解析最新 Release'
   version="${resolved#"$release/tag/"}"
  fi
  [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || die '版本格式无效'
  package="aegishook_${version}_${os}_${arch}"; archive="$package.tar.gz"
  curl "${curl_args[@]}" -o "$tmp/$archive" "$release/download/$version/$archive"
  curl "${curl_args[@]}" -o "$tmp/SHA256SUMS.txt" "$release/download/$version/SHA256SUMS.txt"
  expected="$(awk -v name="$archive" '$2==name || $2=="*"name {print $1}' "$tmp/SHA256SUMS.txt")"
  actual="$("${hash[@]}" "$tmp/$archive" | awk '{print $1}')"
  [[ "$expected" =~ ^[0-9a-f]{64}$ && "$expected" == "$actual" ]] || die '发布包 SHA-256 校验失败'
  tar -xOzf "$tmp/$archive" "$package/aegishook" > "$tmp/aegishook"
  fi
  source_binary="$tmp/aegishook";chmod 700 "$source_binary"
 fi
 [[ -f "$source_binary" ]] || die '找不到 --binary 指定的程序'
 # Check protocol capability before creating any Hook configuration.
 "$source_binary" client install -h >/dev/null || die '程序不支持客户端安装，请使用包含 client 命令的版本'
 mkdir -p -- "$client_dir"
 [[ ! -L "$binary" && ! -e "$binary" && ! -L "$marker" && ! -e "$marker" ]] || die '程序或归属文件已存在，未覆盖'
 cp -- "$source_binary" "$client_dir/.aegishook-new"
 chmod 700 "$client_dir/.aegishook-new"
 mv -- "$client_dir/.aegishook-new" "$binary"
 "${hash[@]}" "$binary" | awk '{print $1}' > "$marker"
fi
[[ ! -L "$binary" && ! -L "$marker" && -f "$marker" ]] || die '程序归属无法确认，未修改'
[[ "$("${hash[@]}" "$binary" | awk '{print $1}')" == "$(cat "$marker")" ]] || die '客户端程序已被修改，未执行'
args=(client "$action" --client-dir "$client_dir" --agent "$agent" --scope "$scope")
if [[ -n "$project" ]];then args+=(--project "$project");fi
if [[ "$action" == install ]];then
 args+=(--endpoint "$endpoint")
 if "$switch_scope";then args+=(--switch);fi
 if [[ -n "$code_file" ]];then
  "$binary" "${args[@]}" --code-file "$code_file"
 else
  "$binary" "${args[@]}"
 fi
else
 "$binary" "${args[@]}"
 if [[ ! -e "$client_dir/client.json" ]];then
  rm -- "$binary" "$marker"
  printf '全部 Hook 已卸载。客户端目录中的锁文件及非自有文件保留。\n'
 fi
fi
