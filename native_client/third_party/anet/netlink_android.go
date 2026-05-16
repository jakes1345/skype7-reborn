//go:build android
// +build android

// Netlink route message helpers for the Android interface enumerator.
// Ported from github.com/wlynxg/anet v0.0.5 netlink_android.go. The
// local helper is kept (rather than using the syscall-package version)
// because recent Go upstream restricted the in-net use of it.

package anet

import (
	"syscall"
	"unsafe"
)

func nlmAlignOf(msglen int) int {
	return (msglen + syscall.NLMSG_ALIGNTO - 1) & ^(syscall.NLMSG_ALIGNTO - 1)
}

type netlinkRouteRequest struct {
	Header syscall.NlMsghdr
	Data   syscall.RtGenmsg
}

func (rr *netlinkRouteRequest) toWireFormat() []byte {
	b := make([]byte, rr.Header.Len)
	*(*uint32)(unsafe.Pointer(&b[0:4][0])) = rr.Header.Len
	*(*uint16)(unsafe.Pointer(&b[4:6][0])) = rr.Header.Type
	*(*uint16)(unsafe.Pointer(&b[6:8][0])) = rr.Header.Flags
	*(*uint32)(unsafe.Pointer(&b[8:12][0])) = rr.Header.Seq
	*(*uint32)(unsafe.Pointer(&b[12:16][0])) = rr.Header.Pid
	b[16] = byte(rr.Data.Family)
	return b
}

func newNetlinkRouteRequest(proto, seq, family int) []byte {
	rr := &netlinkRouteRequest{}
	rr.Header.Len = uint32(syscall.NLMSG_HDRLEN + syscall.SizeofRtGenmsg)
	rr.Header.Type = uint16(proto)
	rr.Header.Flags = syscall.NLM_F_DUMP | syscall.NLM_F_REQUEST
	rr.Header.Seq = uint32(seq)
	rr.Data.Family = uint8(family)
	return rr.toWireFormat()
}

func netlinkRIB(proto, family int) ([]byte, error) {
	s, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(s)
	sa := &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}

	wb := newNetlinkRouteRequest(proto, 1, family)
	if err := syscall.Sendto(s, wb, 0, sa); err != nil {
		return nil, err
	}
	lsa, err := syscall.Getsockname(s)
	if err != nil {
		return nil, err
	}
	lsanl, ok := lsa.(*syscall.SockaddrNetlink)
	if !ok {
		return nil, syscall.EINVAL
	}
	var tab []byte
	rbNew := make([]byte, syscall.Getpagesize())
done:
	for {
		rb := rbNew
		nr, _, err := syscall.Recvfrom(s, rb, 0)
		if err != nil {
			return nil, err
		}
		if nr < syscall.NLMSG_HDRLEN {
			return nil, syscall.EINVAL
		}
		rb = rb[:nr]
		tab = append(tab, rb...)
		msgs, err := syscall.ParseNetlinkMessage(rb)
		if err != nil {
			return nil, err
		}
		for _, m := range msgs {
			if m.Header.Seq != 1 || m.Header.Pid != lsanl.Pid {
				return nil, syscall.EINVAL
			}
			if m.Header.Type == syscall.NLMSG_DONE {
				break done
			}
			if m.Header.Type == syscall.NLMSG_ERROR {
				return nil, syscall.EINVAL
			}
		}
	}
	_ = nlmAlignOf // kept for parity with upstream; unused here
	return tab, nil
}
