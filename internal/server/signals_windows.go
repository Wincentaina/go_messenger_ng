//go:build windows

package server

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

// handleSignals blocks waiting for OS signals.
// Windows does not have SIGHUP; only SIGTERM and SIGINT are handled.
func (s *Server) handleSignals(ln net.Listener) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	for sig := range sigCh {
		switch sig {
		case syscall.SIGTERM, syscall.SIGINT:
			log.Println("shutdown signal received")
			s.shutdown(ln)
			return nil
		}
	}
	return nil
}
