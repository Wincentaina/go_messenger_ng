//go:build windows

package main

// drainStdin is a no-op on Windows: tcell uses the Windows Console API
// and does not leave mouse/focus escape sequences in stdin.
func drainStdin() {}

// resetTerminal is a no-op on Windows for the same reason as drainStdin.
func resetTerminal() {}
