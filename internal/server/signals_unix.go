//go:build !windows

package server

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

// handleSignals blocks waiting for OS signals.
// On SIGHUP it reloads config; on SIGTERM/SIGINT it shuts down gracefully.
func (s *Server) handleSignals(ln net.Listener) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	for sig := range sigCh {
		switch sig {
		case syscall.SIGHUP:
			// Reload config on SIGHUP without downtime
			log.Println("SIGHUP: reloading config (feature stub)")
			s.logger.Log("CONFIG_RELOAD", "", "SIGHUP received")

		case syscall.SIGTERM, syscall.SIGINT:
			log.Println("shutdown signal received")
			s.shutdown(ln)
			return nil
		}
	}
	return nil
}
