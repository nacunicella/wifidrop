//go:build windows

package main

import (
	"syscall"
)

// reusePort lets several WiFiDrop processes on one machine share the
// discovery port. On Windows SO_REUSEADDR alone allows that.
func reusePort(network, address string, c syscall.RawConn) error {
	var serr error
	err := c.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	})
	if err != nil {
		return err
	}
	return serr
}
