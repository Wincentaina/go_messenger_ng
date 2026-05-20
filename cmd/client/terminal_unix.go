//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// drainStdin discards all bytes currently in the stdin buffer without blocking.
func drainStdin() {
	fd := int(os.Stdin.Fd())
	if err := syscall.SetNonblock(fd, true); err != nil {
		return
	}
	buf := make([]byte, 256)
	for {
		n, _ := syscall.Read(fd, buf)
		if n <= 0 {
			break
		}
	}
	syscall.SetNonblock(fd, false) //nolint:errcheck
}

// resetTerminal disables mouse/focus event reporting that tcell may have left
// active after an unclean exit, and drains any pending stdin bytes.
// Two-phase drain: emit disable sequences → wait for terminal to process →
// drain again to catch any events that arrived during the window.
func resetTerminal() {
	// Disable the most common terminal mouse/focus modes.
	fmt.Print("\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1004l\x1b[?1006l\x1b[?1015l")
	drainStdin()

	// Give the terminal emulator time to process the above sequences
	// and deliver any buffered events before we drain again.
	time.Sleep(80 * time.Millisecond)
	drainStdin()
}
