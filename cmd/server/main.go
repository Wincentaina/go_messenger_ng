package main

import (
	"flag"
	"log"
	"os"

	"github.com/wincentaina/go_messenger_ng/internal/crypto"
	"github.com/wincentaina/go_messenger_ng/internal/db"
	"github.com/wincentaina/go_messenger_ng/internal/server"
	"github.com/wincentaina/go_messenger_ng/internal/server/config"
)

func main() {
	cfgPath := flag.String("config", "config/server.yaml", "path to server config")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// DSN can be overridden by environment variable (used in docker-compose)
	dsn := cfg.Database.DSN
	if envDSN := os.Getenv("MESSENGER_DB_DSN"); envDSN != "" {
		dsn = envDSN
	}

	database, err := db.Open(dsn, cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer database.Close()

	logger, err := server.NewLogger(cfg.Logging.File)
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	defer logger.Close()

	tlsCfg, err := crypto.ServerTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile)
	if err != nil {
		log.Fatalf("TLS: %v", err)
	}

	srv := server.New(cfg, database, logger)
	if err := srv.Run(tlsCfg); err != nil {
		log.Fatalf("server: %v", err)
	}
}
