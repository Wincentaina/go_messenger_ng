// Package crypto handles TLS configuration for server and client.
package crypto

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// ServerTLS loads the server's certificate and key, returning a TLS config
// suitable for net.Listen / tls.NewListener.
func ServerTLS(certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load server cert: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// ClientTLS builds a TLS config that trusts the given self-signed CA cert.
// Pass skipVerify=true only in tests.
func ClientTLS(caCertFile string, skipVerify bool) (*tls.Config, error) {
	if skipVerify {
		return &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}, nil //nolint:gosec
	}

	pem, err := os.ReadFile(caCertFile)
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("parse CA cert: no valid certs in %s", caCertFile)
	}

	return &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS13,
	}, nil
}
