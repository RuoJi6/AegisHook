// Package localaddr defines the loopback hosts accepted by the server and hooks.
package localaddr

import (
	"net/netip"
	"strings"
)

func IsLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Zone() == "" && ip.Unmap().IsLoopback()
}
