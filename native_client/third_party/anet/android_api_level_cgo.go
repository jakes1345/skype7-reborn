//go:build android && cgo
// +build android,cgo

package anet

// #include <android/api-level.h>
import "C"

import "sync"

var (
	apiLevel int
	once     sync.Once
)

func androidDeviceApiLevel() int {
	once.Do(func() {
		apiLevel = int(C.android_get_device_api_level())
	})
	return apiLevel
}
