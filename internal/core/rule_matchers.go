package core

import (
	"net/url"
	"path"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

var diskDevice = regexp.MustCompile(`^/dev/(?:[sv]d[a-z]+|xvd[a-z]+|nvme[0-9]+n[0-9]+|mmcblk[0-9]+|r?disk[0-9]+)(?:[sp]?[0-9]+)?$`)
var dollarQuote = regexp.MustCompile(`^\$(?:[A-Za-z_][A-Za-z0-9_]*)?\$`)
var sqlDrop = regexp.MustCompile(`(?i)^\s*DROP\s+(?:DATABASE|TABLE|SCHEMA|INDEX|VIEW|TABLESPACE|USER|ROLE)\b`)
var sqlTruncate = regexp.MustCompile(`(?i)^\s*TRUNCATE\s+\S`)
var sqlDelete = regexp.MustCompile(`(?i)^\s*DELETE\s+FROM\s+\S`)
var sqlWhere = regexp.MustCompile(`(?i)\bWHERE\b`)
var mongoDrop = regexp.MustCompile(`\bdb\b[^;\n]{0,300}\.(?:dropDatabase|dropCollection|drop)\s*\(`)
var pythonDelete = regexp.MustCompile(`\b(?:requests|httpx|aiohttp|urllib\.request|session|client)\s*\.\s*delete\s*\(`)
var axiosDelete = regexp.MustCompile(`\baxios\s*\.\s*delete\s*\(`)
var scriptRequest = regexp.MustCompile(`\b(?:fetch\s*\(|axios\s*(?:\(|\.\s*request\s*\()|(?:requests|httpx)\s*\.\s*request\s*\()`)
var deleteMethod = regexp.MustCompile(`(?i)\bmethod\s*[:=]\s*(['"])DELETE['"]`)

func detailedBuiltinMatches(in ReviewInput, cwd string, resolve func(string, string) string) map[string]string {
	hits := map[string]string{}
	add := func(id string) { hits[id] = "命中明确的命令或请求条件" }
	name := strings.ToLower(in.ToolName)
	checkHTTP := func(method, address string) {
		method = strings.ToUpper(method)
		u, err := url.Parse(address)
		if err != nil || u.Host == "" || !hasExact(u.Scheme, "http", "https") {
			return
		}
		if method == "DELETE" {
			add("R_HTTP_DELETE")
		}
		if !hasExact(method, "POST", "PUT", "PATCH", "DELETE") {
			return
		}
		for _, segment := range strings.Split(strings.ToLower(u.Path), "/") {
			if hasExact(segment, "clear", "wipe", "flush", "purge", "truncate", "drop", "destroy", "factory-reset", "factory_reset", "reset-all", "reset_all") {
				add("R_HTTP_CLEAR")
			}
		}
	}
	checkSQL := func(query string) {
		for _, statement := range strings.Split(maskCode(query, true), ";") {
			if sqlDrop.MatchString(statement) {
				add("R_DB_DROP")
			}
			if sqlTruncate.MatchString(statement) {
				add("R_DB_TRUNCATE")
			}
			if sqlDelete.MatchString(statement) && !sqlWhere.MatchString(statement) {
				add("R_DB_DELETE")
			}
		}
	}
	checkMongo := func(code string) {
		if mongoDrop.MatchString(maskCode(code, false)) {
			add("R_DB_MONGO")
		}
	}
	checkScript := func(code, language string) {
		masked := maskCode(code, false)
		if language == "python" && pythonDelete.MatchString(masked) {
			add("R_HTTP_PYTHON")
		}
		if language == "javascript" && axiosDelete.MatchString(masked) {
			add("R_HTTP_SCRIPT")
		}
		if scriptRequest.MatchString(masked) {
			for _, loc := range deleteMethod.FindAllStringIndex(code, -1) {
				// The method key must be code, not part of a comment/string sample.
				if strings.TrimSpace(masked[loc[0]:loc[0]+6]) != "" {
					add("R_HTTP_SCRIPT")
				}
			}
		}
	}
	checkHTTP(argumentText(in.Arguments, "method"), argumentText(in.Arguments, "url"))
	if has(name, "sql", "database", "query") && !has(name, "mongo", "redis") {
		checkSQL(argumentText(in.Arguments, "query", "sql", "statement", "command"))
	}
	if strings.Contains(name, "mongo") {
		checkMongo(argumentText(in.Arguments, "query", "script", "command"))
	}
	if strings.Contains(name, "redis") {
		if fields := strings.Fields(argumentText(in.Arguments, "command", "cmd")); len(fields) > 0 && hasExact(strings.ToUpper(fields[0]), "FLUSHALL", "FLUSHDB") {
			add("R_DB_REDIS")
		}
	}
	if hasExact(name, "python", "execute_python", "run_python") {
		checkScript(argumentText(in.Arguments, "code", "script"), "python")
	}
	if hasExact(name, "javascript", "execute_javascript", "run_javascript", "node") {
		checkScript(argumentText(in.Arguments, "code", "script"), "javascript")
	}

	var inspectShell func(string, int)
	inspectShell = func(command string, depth int) {
		if command == "" || depth > 3 {
			return
		}
		tree, err := syntax.NewParser().Parse(strings.NewReader(command), "")
		if err != nil {
			return
		}
		forkFunctions := map[string]bool{}
		syntax.Walk(tree, func(n syntax.Node) bool {
			if fn, ok := n.(*syntax.FuncDecl); ok {
				syntax.Walk(fn.Body, func(n syntax.Node) bool {
					stmt, ok := n.(*syntax.Stmt)
					if !ok || !stmt.Background {
						return true
					}
					pipe, ok := stmt.Cmd.(*syntax.BinaryCmd)
					if ok && pipe.Op == syntax.Pipe && callName(pipe.X) == fn.Name.Value && callName(pipe.Y) == fn.Name.Value {
						forkFunctions[fn.Name.Value] = true
					}
					return true
				})
				return false
			}
			return true
		})
		syntax.Walk(tree, func(n syntax.Node) bool {
			if _, ok := n.(*syntax.FuncDecl); ok {
				return false
			}
			if red, ok := n.(*syntax.Redirect); ok && hasOutput(red.Op) && diskDevice.MatchString(literalWord(red.Word)) {
				add("R_SYS_DD")
			}
			if pipe, ok := n.(*syntax.BinaryCmd); ok && pipe.Op == syntax.Pipe && networkCommand(callName(pipe.X)) && networkCommand(callName(pipe.Y)) {
				add("R_NET_PIPE")
			}
			call, ok := n.(*syntax.CallExpr)
			if !ok {
				return true
			}
			words := callWords(call)
			if len(words) == 0 {
				return true
			}
			exe, args := strings.ToLower(path.Base(words[0])), words[1:]
			if forkFunctions[words[0]] {
				add("R_SYS_FORK")
			}
			if hasExact(exe, "sh", "bash", "zsh", "dash") {
				if script := optionValue(args, "-c", "-lc", "-cl"); script != "" {
					inspectShell(script, depth+1)
				}
			}
			// Queries for CLI help/version do not execute the requested effect.
			if helpOnly(args) {
				return true
			}
			switch {
			case exe == "rm":
				recursive, operand := false, false
				options := true
				for _, arg := range args {
					if options && arg == "--" {
						options = false
						continue
					}
					if options && strings.HasPrefix(arg, "-") {
						if hasExact(arg, "--recursive", "--no-preserve-root") || (!strings.HasPrefix(arg, "--") && strings.ContainsAny(arg[1:], "rR")) {
							recursive = true
						}
					} else if arg != "" {
						operand = true
					}
				}
				if recursive && operand {
					add("R_SYS_RM")
				}
			case exe == "mkfs" || strings.HasPrefix(exe, "mkfs.") || hasExact(exe, "format-volume", "clear-disk"):
				add("R_SYS_FORMAT")
			case exe == "diskutil":
				if len(args) > 0 && hasExact(strings.ToLower(args[0]), "erasedisk", "erasevolume", "partitiondisk") {
					add("R_SYS_FORMAT")
				}
			case exe == "dd":
				for _, arg := range args {
					if strings.HasPrefix(arg, "of=") && diskDevice.MatchString(strings.TrimPrefix(arg, "of=")) {
						add("R_SYS_DD")
					}
				}
			case hasExact(exe, "shutdown", "reboot", "halt", "poweroff", "stop-computer", "restart-computer"):
				add("R_SYS_SHUTDOWN")
			case hasExact(exe, "init", "telinit"):
				if len(args) > 0 && hasExact(args[0], "0", "6") {
					add("R_SYS_SHUTDOWN")
				}
			case exe == "kill":
				if containsArg(args, "-1") {
					add("R_SYS_KILL")
				}
			case hasExact(exe, "killall", "pkill"):
				if containsArg(args, "-9", "-KILL", "-SIGKILL") {
					add("R_SYS_KILL")
				}
			case hasExact(exe, "iptables", "ip6tables"):
				if containsArg(args, "-F", "--flush") {
					add("R_SYS_FIREWALL")
				}
			case exe == "nft":
				for i := 0; i+1 < len(args); i++ {
					if args[i] == "flush" && hasExact(args[i+1], "ruleset", "table", "chain") {
						add("R_SYS_FIREWALL")
					}
				}
			case exe == "ufw":
				if containsArg(args, "reset") {
					add("R_SYS_FIREWALL")
				}
			case hasExact(exe, "mysql", "mariadb", "psql", "sqlcmd"):
				checkSQL(optionValue(args, "-e", "--execute", "-c", "--command", "-Q", "-q"))
			case exe == "sqlite3":
				// The final argument is inline SQL, except a lone database filename.
				if len(args) > 1 {
					checkSQL(args[len(args)-1])
				}
			case hasExact(exe, "mongo", "mongosh"):
				checkMongo(optionValue(args, "--eval"))
			case exe == "redis-cli":
				if hasExact(strings.ToUpper(redisCommand(args)), "FLUSHALL", "FLUSHDB") {
					add("R_DB_REDIS")
				}
			case hasExact(exe, "python", "python3", "python2") || strings.HasPrefix(exe, "python3."):
				checkScript(optionValue(args, "-c"), "python")
			case hasExact(exe, "node", "nodejs"):
				checkScript(optionValue(args, "-e", "--eval", "-p", "--print"), "javascript")
			}
			if hasExact(exe, "rm", "rmdir", "shred", "wipe") {
				for _, arg := range args {
					if arg == "" || strings.HasPrefix(arg, "-") {
						continue
					}
					p := strings.ReplaceAll(resolve(arg, cwd), "\\", "/")
					for _, root := range []string{"/", "/etc", "/bin", "/usr", "/boot", "/var", "/lib", "/lib64", "/sys", "/proc", "/dev", "/sbin", "/root", "/home", "/Users", "/System", "/Library", "/private/etc", "/private/var"} {
						if p == root || (root != "/" && strings.HasPrefix(p, root+"/")) {
							add("R_SYS_PATH")
						}
					}
				}
			}
			if hasExact(exe, "shred", "wipe", "wipefs") && !(exe == "wipefs" && containsArg(args, "-n", "--no-act")) {
				for _, arg := range args {
					if diskDevice.MatchString(arg) {
						add("R_SYS_WIPE")
					}
				}
			}
			if hasExact(exe, "curl", "wget", "invoke-webrequest", "invoke-restmethod", "iwr", "irm") {
				method, addresses := httpCommand(exe, args)
				for _, address := range addresses {
					checkHTTP(method, address)
				}
			}
			return true
		})
	}
	inspectShell(argumentText(in.Arguments, "command", "cmd"), 0)
	return hits
}

func hasOutput(op syntax.RedirOperator) bool {
	return op == syntax.RdrOut || op == syntax.AppOut || op == syntax.RdrAll || op == syntax.AppAll
}
func containsArg(args []string, options ...string) bool {
	for _, arg := range args {
		if hasExact(arg, options...) {
			return true
		}
	}
	return false
}
func helpOnly(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if hasExact(arg, "--help", "--version") {
			return true
		}
	}
	return false
}
func optionValue(args []string, options ...string) string {
	value := ""
	for i, arg := range args {
		if arg == "--" {
			break
		}
		for _, opt := range options {
			if arg == opt && i+1 < len(args) {
				value = args[i+1]
			}
			if strings.HasPrefix(opt, "--") && strings.HasPrefix(arg, opt+"=") {
				value = strings.TrimPrefix(arg, opt+"=")
			}
			if len(opt) == 2 && strings.HasPrefix(arg, opt) && len(arg) > 2 {
				value = arg[2:]
			}
		}
	}
	return value
}
func callWords(call *syntax.CallExpr) []string {
	var words []string
	for _, w := range call.Args {
		words = append(words, literalWord(w))
	}
	for len(words) > 1 {
		exe := strings.ToLower(path.Base(words[0]))
		if !hasExact(exe, "sudo", "env", "command") {
			break
		}
		i := 1
		for i < len(words) {
			arg := words[i]
			if arg == "--" {
				i++
				break
			}
			if exe == "env" && strings.Contains(arg, "=") {
				i++
				continue
			}
			if exe == "sudo" && hasExact(arg, "-u", "-g", "-h", "-p", "-C", "-T") {
				i += 2
				continue
			}
			if hasExact(arg, "-n", "-E", "-H", "-i") && exe == "sudo" {
				i++
				continue
			}
			// Unknown wrapper options fall through to review rather than guessing.
			if strings.HasPrefix(arg, "-") {
				return nil
			}
			break
		}
		if i >= len(words) {
			return nil
		}
		words = words[i:]
	}
	return words
}
func callName(stmt *syntax.Stmt) string {
	if call, ok := stmt.Cmd.(*syntax.CallExpr); ok {
		if words := callWords(call); len(words) > 0 {
			return path.Base(words[0])
		}
	}
	return ""
}
func networkCommand(name string) bool { return hasExact(name, "curl", "wget", "nc", "ncat", "netcat") }
func redisCommand(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" && i+1 < len(args) {
			return args[i+1]
		}
		if hasExact(arg, "-h", "-p", "-s", "-a", "--pass", "--user", "-n", "-u", "--uri", "--cacert", "--cert", "--key", "--sni", "-r", "-i") {
			i++
			continue
		}
		if hasExact(arg, "--tls", "--raw", "--no-raw", "--csv", "--json", "--quoted-json", "-c", "-x", "--no-auth-warning") {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return ""
		}
		return arg
	}
	return ""
}

// Preserve byte offsets while masking quoted literals/comments. This is a
// deliberately bounded lexer, not a SQL/Python/JavaScript interpreter.
func maskCode(code string, sql bool) string {
	out := []byte(code)
	blank := func(start, end int) {
		for j := start; j < end; j++ {
			if out[j] != '\n' && out[j] != '\r' {
				out[j] = ' '
			}
		}
	}
	for i := 0; i < len(code); {
		start := i
		if sql && code[i] == '$' {
			if delimiter := dollarQuote.FindString(code[i:]); delimiter != "" {
				i += len(delimiter)
				if end := strings.Index(code[i:], delimiter); end >= 0 {
					i += end + len(delimiter)
				} else {
					i = len(code)
				}
				blank(start, i)
				continue
			}
		}
		if code[i] == '#' || (i+1 < len(code) && (code[i:i+2] == "//" || (sql && code[i:i+2] == "--"))) {
			for i < len(code) && code[i] != '\n' {
				i++
			}
			blank(start, i)
		} else if strings.HasPrefix(code[i:], "/*") {
			i += 2
			for i < len(code) && !strings.HasPrefix(code[i:], "*/") {
				i++
			}
			i = min(len(code), i+2)
			blank(start, i)
		} else if code[i] == '\'' || code[i] == '"' || code[i] == '`' {
			quote := code[i : i+1]
			if !sql && strings.HasPrefix(code[i:], strings.Repeat(quote, 3)) {
				quote = strings.Repeat(quote, 3)
			}
			i += len(quote)
			for i < len(code) {
				if code[i] == '\\' {
					i = min(len(code), i+2)
					continue
				}
				if strings.HasPrefix(code[i:], quote) {
					i += len(quote)
					if sql && strings.HasPrefix(code[i:], quote) {
						i += len(quote)
						continue
					}
					break
				}
				i++
			}
			blank(start, i)
			if sql && code[start] != '\'' {
				out[start] = 'x'
			} // Quoted identifier, not a string constant.
		} else {
			i++
		}
	}
	return string(out)
}

// Consume option values before examining URLs. A URL in a header/request body
// is data, and must not be mistaken for the request destination.
func httpCommand(exe string, args []string) (string, []string) {
	method, explicit := "GET", ""
	var addresses []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			addresses = append(addresses, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") {
			addresses = append(addresses, arg)
			continue
		}
		opt, value, attached := arg, "", false
		if k := strings.IndexByte(arg, '='); k >= 0 {
			opt, value, attached = arg[:k], arg[k+1:], true
		}
		if exe == "curl" && !strings.HasPrefix(opt, "--") && len(opt) > 2 && strings.ContainsAny(opt[1:2], "XdFTHuobAe") {
			opt, value, attached = opt[:2], opt[2:], true
		}
		if exe != "curl" && exe != "wget" {
			opt = strings.ToLower(opt)
		}
		takesValue := false
		switch exe {
		case "curl":
			takesValue = hasExact(opt, "-X", "--request", "--url", "-d", "--data", "--data-raw", "--data-binary", "--data-urlencode", "--json", "-F", "--form", "-T", "--upload-file", "-H", "--header", "-u", "--user", "-o", "--output", "-b", "--cookie", "-A", "--user-agent", "-e", "--referer", "--proxy", "--connect-timeout", "--max-time", "--resolve", "--cacert", "--cert", "--key", "--config", "-K")
		case "wget":
			takesValue = hasExact(opt, "--method", "--post-data", "--post-file", "--body-data", "--body-file", "--header", "--user", "--password", "--output-document", "-O", "--timeout", "-T", "--input-file", "-i")
		default:
			takesValue = hasExact(opt, "-method", "-uri", "-body", "-headers", "-outfile", "-contenttype", "-credential", "-timeoutsec")
		}
		if takesValue && !attached && i+1 < len(args) {
			i++
			value = args[i]
		}
		if (exe == "curl" && hasExact(opt, "-X", "--request")) || (exe == "wget" && opt == "--method") || opt == "-method" {
			explicit = value
		}
		if hasExact(opt, "--url", "-uri") {
			addresses = append(addresses, value)
		}
		if (exe == "curl" && hasExact(opt, "-d", "--data", "--data-raw", "--data-binary", "--data-urlencode", "--json", "-F", "--form")) || (exe == "wget" && hasExact(opt, "--post-data", "--post-file")) {
			method = "POST"
		}
		if exe == "curl" && hasExact(opt, "-T", "--upload-file") {
			method = "PUT"
		}
		if exe == "curl" && hasExact(opt, "-I", "--head") {
			method = "HEAD"
		}
	}
	if explicit != "" {
		method = explicit
	}
	return method, addresses
}
