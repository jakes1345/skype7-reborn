//go:build !android
// +build !android

// Package anet is a local shim replacing github.com/wlynxg/anet.
// Upstream uses //go:linkname to net.zoneCache which Go 1.23+ refuses
// with "invalid reference to net.zoneCache", breaking fyne-cross
// android builds. This shim ports upstream's netlink-based Android
// implementation minus the zoneCache optimization, and falls through
// to stdlib net on every other platform.
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
