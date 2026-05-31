//go:build darwin

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// enterCbreak puts stdin into cbreak mode: keys arrive unbuffered (so we can see
// Escape immediately) with echo off, but signals (Ctrl-C) and output newline
// translation are left intact. Returns ok=false when stdin isn't a terminal.
func enterCbreak() (restore func(), ok bool) {
	fd := int(os.Stdin.Fd())
	old, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return func() {}, false // not a terminal (piped/redirected)
	}
	raw := *old
	raw.Lflag &^= unix.ICANON | unix.ECHO // unbuffered input, no echo; keep ISIG
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TIOCSETA, &raw); err != nil {
		return func() {}, false
	}
	return func() { _ = unix.IoctlSetTermios(fd, unix.TIOCSETA, old) }, true
}
