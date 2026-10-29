//go:build !windows

package main

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// reusePort lets several WiFiDrop processes on one machine share the
// discovery port, so each of them receives the broadcast beacons.
func reusePort(network, address string, c syscall.RawConn) error {
	var serr error
	err := c.Control(func(fd uintptr) {
		serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
		if serr == nil {
			serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		}
	})
	if err != nil {
		return err
	}
	return serr
}
