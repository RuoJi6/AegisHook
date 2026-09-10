package localaddr

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Endpoint permits HTTPS services and HTTP over a local loopback/tunnel only.
func Endpoint(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("风控地址须为 HTTPS 源站地址（不含凭据、路径、查询或片段）；本机回环地址可使用 HTTP")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && IsLoopback(u.Hostname())) {
		return "", errors.New("远程风控地址必须使用 HTTPS；HTTP 仅允许本机回环地址")
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("风控地址端口无效")
		}
	}
	return strings.TrimRight(u.String(), "/"), nil
}
