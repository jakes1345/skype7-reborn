//go:build !(android && cgo)
// +build !android !cgo

package anet

func androidDeviceApiLevel() int {
	return -1
}
