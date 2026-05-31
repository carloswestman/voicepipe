//go:build !darwin

package main

// enterCbreak is a no-op off macOS; the Esc-to-stop feature is simply disabled.
func enterCbreak() (restore func(), ok bool) {
	return func() {}, false
}
