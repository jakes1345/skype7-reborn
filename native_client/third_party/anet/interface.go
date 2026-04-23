// Package anet is a local shim replacing github.com/wlynxg/anet.
//
// Upstream anet uses //go:linkname to hook into net.zoneCache on Android,
// which Go 1.23+ rejects with `link: invalid reference to net.zoneCache`
// and breaks fyne-cross android builds. On real phones we don't need the
// containerized-Android IPv6 zone fix anet was designed for — the stdlib
// passthrough works fine. This shim just calls stdlib net.* on every
// platform (mirroring upstream's non-Android behavior).
package anet

import "net"

func Interfaces() ([]net.Interface, error) {
	return net.Interfaces()
}

func InterfaceAddrs() ([]net.Addr, error) {
	return net.InterfaceAddrs()
}

func InterfaceAddrsByInterface(ifi *net.Interface) ([]net.Addr, error) {
	return ifi.Addrs()
}

func SetAndroidVersion(version uint) {}
