package safety

import (
	"net"
	"strings"
)

func UnsafeNoTokenNonLoopbackBind(addr string, hasToken bool) bool {
	if hasToken {
		return false
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return true
	}
	if ip.IsUnspecified() {
		return true
	}
	return !ip.IsLoopback()
}
