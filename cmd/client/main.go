package main

import (
	"bufio"
	"crypto/tls"
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
	cfgPath   := flag.String("config",   "config/client.yaml", "path to client config")
	flagUser  := flag.String("user",     "", "username (skips interactive prompt)")
	flagPass  := flag.String("pass",     "", "password (skips interactive prompt)")
	flagReg   := flag.Bool("register",   false, "register a new account")
	flagNoTLS := flag.Bool("no-tls",     false, "disable TLS (plain TCP, for debugging only)")
	flag.Parse()

	// Redirect all log output to a file so it doesn't corrupt the tview TUI.
	if f, err := os.OpenFile("logs/client.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}

	// tcell (used by tview) enables mouse/focus event reporting and may not
	// disable it if the previous session was killed without proper cleanup.
	// Emit the corresponding disable sequences and drain any stale stdin bytes
	// so they don't corrupt the credential prompt input.
	resetTerminal()

	cfg, err := clientcfg.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	var tlsCfg *tls.Config
	if !*flagNoTLS {
		tlsCfg, err = crypto.ClientTLS(cfg.TLS.CACert, cfg.TLS.SkipVerify)
		if err != nil {
			log.Fatalf("TLS: %v", err)
		}
	}

	// Single stdin reader shared across retries — avoids losing buffered data
	// when a new bufio.Reader is created on each iteration.
	stdinReader := bufio.NewReader(os.Stdin)

	// Credentials are collected BEFORE connecting: cleaner UX and avoids
	// a timing window where the server waits while the user is typing.
	var (
		conn     *client.Conn
		username string
	)

	if *flagUser != "" && *flagPass != "" {
		// Non-interactive mode: credentials supplied via flags.
		username = sanitizeASCII(*flagUser)
		password := sanitizeASCII(*flagPass)

		var err error
		conn, err = client.Connect(cfg.Server.Address, tlsCfg)
		if err != nil {
			log.Fatalf("подключение не удалось: %v", err)
		}
		resp, err := conn.Auth(username, password, *flagReg)
		if err != nil {
			conn.Close()
			log.Fatalf("ошибка соединения: %v", err)
		}
		if !resp.OK {
			conn.Close()
			msg := resp.Message
			if msg == "" {
				msg = "неверные данные"
			}
			log.Fatalf("авторизация не удалась: %s", msg)
		}
	} else {
		// Interactive mode: prompt for credentials, retry on failure.
		for {
			var password string
			var register bool
			username, password, register = promptCredentials(stdinReader)

			var err error
			conn, err = client.Connect(cfg.Server.Address, tlsCfg)
			if err != nil {
				log.Fatalf("подключение не удалось: %v", err)
			}

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
	}
	defer conn.Close()

	cache := client.NewMessageCache(cfg.Cache.MaxMessagesPerChat)
	app := ui.New(conn, username, cache)

	if err := app.Run(); err != nil {
		log.Fatalf("TUI: %v", err)
	}
}


func promptCredentials(r *bufio.Reader) (username, password string, register bool) {
	fmt.Print("Имя пользователя: ")
	username, _ = r.ReadString('\n')
	username = sanitizeASCII(strings.TrimSpace(username))

	fmt.Print("Пароль: ")
	password, _ = r.ReadString('\n')
	password = sanitizeASCII(strings.TrimSpace(password))

	fmt.Print("Регистрация нового аккаунта? (y/N): ")
	ans, _ := r.ReadString('\n')
	register = strings.TrimSpace(strings.ToLower(ans)) == "y"
	return
}

// sanitizeASCII strips any non-printable or non-ASCII bytes that may have
// been injected into stdin by terminal mouse/focus events from a previous
// tview session (e.g. garbage prefix like "\xd1\x89\xaa" before the actual input).
func sanitizeASCII(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c < 0x7F {
			b.WriteByte(c)
		}
	}
	return b.String()
}
