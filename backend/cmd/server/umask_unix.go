//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package main

import "syscall"

func restrictProcessFilePermissions() {
	syscall.Umask(0o077)
}
