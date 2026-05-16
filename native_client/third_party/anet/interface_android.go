//go:build android
// +build android

// Android-specific network interface enumeration ported from upstream
// github.com/wlynxg/anet v0.0.5, with the //go:linkname-to-net.zoneCache
// optimization removed (Go 1.23+ refuses it). Functional behavior
// preserved: pion/transport calls Interfaces() and
// InterfaceAddrsByInterface() on startup to pick ICE candidates, and on
// Android 11+ stdlib net.Interfaces() returns an empty list due to
// upstream Go's netlink permissions. Without this implementation the
// app crashes at launch.

package anet

import (
	"errors"
	"net"
	"os"
	"syscall"
	"unsafe"
)

const android11ApiLevel = 30

var (
	customAndroidApiLevel = -1
	errInvalidInterface   = errors.New("invalid network interface")
)

type ifReq [40]byte

func Interfaces() ([]net.Interface, error) {
	if androidApiLevel() < android11ApiLevel {
		return net.Interfaces()
	}
	ift, err := interfaceTable(0)
	if err != nil {
		return nil, &net.OpError{Op: "route", Net: "ip+net", Err: err}
	}
	return ift, nil
}

func InterfaceAddrs() ([]net.Addr, error) {
	if androidApiLevel() < android11ApiLevel {
		return net.InterfaceAddrs()
	}
	ifat, err := interfaceAddrTable(nil)
	if err != nil {
		err = &net.OpError{Op: "route", Net: "ip+net", Err: err}
	}
	return ifat, err
}

func InterfaceAddrsByInterface(ifi *net.Interface) ([]net.Addr, error) {
	if ifi == nil {
		return nil, &net.OpError{Op: "route", Net: "ip+net", Err: errInvalidInterface}
	}
	if androidApiLevel() < android11ApiLevel {
		return ifi.Addrs()
	}
	ifat, err := interfaceAddrTable(ifi)
	if err != nil {
		err = &net.OpError{Op: "route", Net: "ip+net", Err: err}
	}
	return ifat, err
}

func SetAndroidVersion(version uint) {
	switch {
	case version == 0:
		customAndroidApiLevel = -1
	case version >= 11:
		customAndroidApiLevel = android11ApiLevel
	default:
		customAndroidApiLevel = 0
	}
}

func androidApiLevel() int {
	if customAndroidApiLevel != -1 {
		return customAndroidApiLevel
	}
	return androidDeviceApiLevel()
}

func interfaceTable(ifindex int) ([]net.Interface, error) {
	tab, err := netlinkRIB(syscall.RTM_GETADDR, syscall.AF_UNSPEC)
	if err != nil {
		return nil, os.NewSyscallError("netlinkrib", err)
	}
	msgs, err := syscall.ParseNetlinkMessage(tab)
	if err != nil {
		return nil, os.NewSyscallError("parsenetlinkmessage", err)
	}
	var ift []net.Interface
	im := make(map[uint32]struct{})
loop:
	for _, m := range msgs {
		switch m.Header.Type {
		case syscall.NLMSG_DONE:
			break loop
		case syscall.RTM_NEWADDR:
			ifam := (*syscall.IfAddrmsg)(unsafe.Pointer(&m.Data[0]))
			if _, ok := im[ifam.Index]; ok {
				continue
			}
			im[ifam.Index] = struct{}{}
			if ifindex == 0 || ifindex == int(ifam.Index) {
				if ifi := newLink(ifam); ifi != nil {
					ift = append(ift, *ifi)
				}
				if ifindex == int(ifam.Index) {
					break loop
				}
			}
		}
	}
	return ift, nil
}

func newLink(ifam *syscall.IfAddrmsg) *net.Interface {
	ift := &net.Interface{Index: int(ifam.Index)}
	name, err := indexToName(ifam.Index)
	if err != nil {
		return nil
	}
	ift.Name = name
	mtu, err := nameToMTU(name)
	if err != nil {
		return nil
	}
	ift.MTU = mtu
	flags, err := nameToFlags(name)
	if err != nil {
		return nil
	}
	ift.Flags = flags
	return ift
}

func linkFlags(rawFlags uint32) net.Flags {
	var f net.Flags
	if rawFlags&syscall.IFF_UP != 0 {
		f |= net.FlagUp
	}
	if rawFlags&syscall.IFF_RUNNING != 0 {
		f |= net.FlagRunning
	}
	if rawFlags&syscall.IFF_BROADCAST != 0 {
		f |= net.FlagBroadcast
	}
	if rawFlags&syscall.IFF_LOOPBACK != 0 {
		f |= net.FlagLoopback
	}
	if rawFlags&syscall.IFF_POINTOPOINT != 0 {
		f |= net.FlagPointToPoint
	}
	if rawFlags&syscall.IFF_MULTICAST != 0 {
		f |= net.FlagMulticast
	}
	return f
}

func interfaceAddrTable(ifi *net.Interface) ([]net.Addr, error) {
	tab, err := netlinkRIB(syscall.RTM_GETADDR, syscall.AF_UNSPEC)
	if err != nil {
		return nil, os.NewSyscallError("netlinkrib", err)
	}
	msgs, err := syscall.ParseNetlinkMessage(tab)
	if err != nil {
		return nil, os.NewSyscallError("parsenetlinkmessage", err)
	}
	var ift []net.Interface
	if ifi == nil {
		var err error
		ift, err = interfaceTable(0)
		if err != nil {
			return nil, err
		}
	}
	return addrTable(ift, ifi, msgs)
}

func addrTable(ift []net.Interface, ifi *net.Interface, msgs []syscall.NetlinkMessage) ([]net.Addr, error) {
	var ifat []net.Addr
loop:
	for _, m := range msgs {
		switch m.Header.Type {
		case syscall.NLMSG_DONE:
			break loop
		case syscall.RTM_NEWADDR:
			ifam := (*syscall.IfAddrmsg)(unsafe.Pointer(&m.Data[0]))
			if len(ift) != 0 || ifi.Index == int(ifam.Index) {
				attrs, err := syscall.ParseNetlinkRouteAttr(&m)
				if err != nil {
					return nil, os.NewSyscallError("parsenetlinkrouteattr", err)
				}
				if ifa := newAddr(ifam, attrs); ifa != nil {
					ifat = append(ifat, ifa)
				}
			}
		}
	}
	return ifat, nil
}

func newAddr(ifam *syscall.IfAddrmsg, attrs []syscall.NetlinkRouteAttr) net.Addr {
	var ipPointToPoint bool
	for _, a := range attrs {
		if a.Attr.Type == syscall.IFA_LOCAL {
			ipPointToPoint = true
			break
		}
	}
	for _, a := range attrs {
		if ipPointToPoint && a.Attr.Type == syscall.IFA_ADDRESS {
			continue
		}
		switch ifam.Family {
		case syscall.AF_INET:
			return &net.IPNet{IP: net.IPv4(a.Value[0], a.Value[1], a.Value[2], a.Value[3]), Mask: net.CIDRMask(int(ifam.Prefixlen), 8*net.IPv4len)}
		case syscall.AF_INET6:
			ifa := &net.IPNet{IP: make(net.IP, net.IPv6len), Mask: net.CIDRMask(int(ifam.Prefixlen), 8*net.IPv6len)}
			copy(ifa.IP, a.Value[:])
			return ifa
		}
	}
	return nil
}

func ioctl(fd int, req uint, arg unsafe.Pointer) error {
	_, _, e1 := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(arg))
	if e1 != 0 {
		return e1
	}
	return nil
}

func indexToName(index uint32) (string, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	defer syscall.Close(fd)
	var ifr ifReq
	*(*uint32)(unsafe.Pointer(&ifr[syscall.IFNAMSIZ])) = index
	if err := ioctl(fd, syscall.SIOCGIFNAME, unsafe.Pointer(&ifr[0])); err != nil {
		return "", err
	}
	// trim trailing NULs
	n := 0
	for n < syscall.IFNAMSIZ && ifr[n] != 0 {
		n++
	}
	return string(ifr[:n]), nil
}

func nameToMTU(name string) (int, error) {
	if len(name) >= syscall.IFNAMSIZ {
		return -1, syscall.EINVAL
	}
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	defer syscall.Close(fd)
	var ifr ifReq
	copy(ifr[:], name)
	if err := ioctl(fd, syscall.SIOCGIFMTU, unsafe.Pointer(&ifr[0])); err != nil {
		return -1, err
	}
	return int(*(*int32)(unsafe.Pointer(&ifr[syscall.IFNAMSIZ]))), nil
}

func nameToFlags(name string) (net.Flags, error) {
	if len(name) >= syscall.IFNAMSIZ {
		return 0, syscall.EINVAL
	}
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return 0, err
	}
	defer syscall.Close(fd)
	var ifr ifReq
	copy(ifr[:], name)
	if err := ioctl(fd, syscall.SIOCGIFFLAGS, unsafe.Pointer(&ifr[0])); err != nil {
		return 0, err
	}
	return linkFlags(*(*uint32)(unsafe.Pointer(&ifr[syscall.IFNAMSIZ]))), nil
}
