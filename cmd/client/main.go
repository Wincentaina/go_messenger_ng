package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/wincentaina/go_messenger_ng/internal/client"
	clientcfg "github.com/wincentaina/go_messenger_ng/internal/client/config"
	"github.com/wincentaina/go_messenger_ng/internal/crypto"
	"github.com/wincentaina/go_messenger_ng/internal/ui"
)

func main() {
	cfgPath := flag.String("config", "config/client.yaml", "path to client config")
	flag.Parse()

	cfg, err := clientcfg.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	tlsCfg, err := crypto.ClientTLS(cfg.TLS.CACert, cfg.TLS.SkipVerify)
	if err != nil {
		log.Fatalf("TLS: %v", err)
	}

	// Auth retry loop: reconnects on each failed attempt (server closes conn on failure).
	var (
		conn     *client.Conn
		username string
	)
	for {
		var err error
		conn, err = client.Connect(cfg.Server.Address, tlsCfg)
		if err != nil {
			log.Fatalf("подключение не удалось: %v", err)
		}

		var password string
		var register bool
		username, password, register = promptCredentials()

		resp, err := conn.Auth(username, password, register)
		if err != nil {
			conn.Close()
			log.Fatalf("ошибка соединения: %v", err)
		}
		if resp.OK {
			break
		}

		conn.Close()
		msg := resp.Message
		if msg == "" {
			msg = "неверные данные"
		}
		fmt.Fprintf(os.Stderr, "Ошибка: %s. Попробуйте снова.\n\n", msg)
	}
	defer conn.Close()

	cache := client.NewMessageCache(cfg.Cache.MaxMessagesPerChat)
	app := ui.New(conn, username, cache)

	if err := app.Run(); err != nil {
		log.Fatalf("TUI: %v", err)
	}
}

func promptCredentials() (username, password string, register bool) {
	r := bufio.NewReader(os.Stdin)

	fmt.Print("Имя пользователя: ")
	username, _ = r.ReadString('\n')
	username = strings.TrimSpace(username)

	fmt.Print("Пароль: ")
	password, _ = r.ReadString('\n')
	password = strings.TrimSpace(password)

	fmt.Print("Регистрация нового аккаунта? (y/N): ")
	ans, _ := r.ReadString('\n')
	register = strings.TrimSpace(strings.ToLower(ans)) == "y"
	return
}
