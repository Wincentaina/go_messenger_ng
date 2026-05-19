package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/wincentaina/go_messenger_ng/internal/client"
	clientcfg "github.com/wincentaina/go_messenger_ng/internal/client/config"
	"github.com/wincentaina/go_messenger_ng/internal/crypto"
	"github.com/wincentaina/go_messenger_ng/internal/protocol"
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

	conn, err := client.Connect(cfg.Server.Address, tlsCfg)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	// --- Auth ---
	username, password, register := promptCredentials()
	resp, err := conn.Auth(username, password, register)
	if err != nil || !resp.OK {
		msg := "auth failed"
		if resp.Message != "" {
			msg = resp.Message
		}
		log.Fatalf("auth: %s", msg)
	}
	fmt.Printf("Добро пожаловать, %s!\n", username)
	printHelp()

	cache := client.NewMessageCache(cfg.Cache.MaxMessagesPerChat)

	// Handle OS signals for clean exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Goroutine: print incoming messages as they arrive
	go func() {
		for {
			select {
			case msg, ok := <-conn.Incoming():
				if !ok {
					return
				}
				handleIncoming(msg, cache, username)
			case <-conn.Done():
				return
			}
		}
	}()

	// Main goroutine: read commands from stdin
	scanner := bufio.NewScanner(os.Stdin)
	inputCh := make(chan string)
	go func() {
		for scanner.Scan() {
			inputCh <- scanner.Text()
		}
	}()

	for {
		fmt.Print("> ")
		select {
		case line := <-inputCh:
			if err := handleInput(line, conn, username); err != nil {
				fmt.Println("ошибка:", err)
			}
		case <-conn.Done():
			fmt.Println("\nСоединение с сервером разорвано.")
			return
		case <-sigCh:
			fmt.Println("\nВыход.")
			return
		}
	}
}

func promptCredentials() (username, password string, register bool) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Имя пользователя: ")
	username, _ = reader.ReadString('\n')
	username = strings.TrimSpace(username)

	fmt.Print("Пароль: ")
	password, _ = reader.ReadString('\n')
	password = strings.TrimSpace(password)

	fmt.Print("Регистрация нового аккаунта? (y/N): ")
	ans, _ := reader.ReadString('\n')
	register = strings.TrimSpace(strings.ToLower(ans)) == "y"
	return
}

func handleInput(line string, conn *client.Conn, me string) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	parts := strings.SplitN(line, " ", 3)
	cmd := parts[0]

	switch cmd {
	case "/msg":
		if len(parts) < 3 {
			return fmt.Errorf("использование: /msg <пользователь> <текст>")
		}
		conn.Send(protocol.TypeSendMsg, protocol.SendMsg{
			ToUser:  parts[1],
			Content: parts[2],
		})

	case "/reply":
		// /reply <message_id> <text>
		if len(parts) < 3 {
			return fmt.Errorf("использование: /reply <id> <текст>")
		}
		var id int64
		fmt.Sscan(parts[1], &id)
		conn.Send(protocol.TypeSendMsg, protocol.SendMsg{
			Content:   parts[2],
			ReplyToID: id,
		})

	case "/history":
		if len(parts) < 2 {
			return fmt.Errorf("использование: /history <пользователь>")
		}
		conn.Send(protocol.TypeHistoryReq, protocol.HistoryReq{
			WithUser: parts[1],
			Limit:    50,
		})

	case "/users":
		conn.Send(protocol.TypeUserListReq, protocol.UserListReq{})

	case "/newgroup":
		if len(parts) < 2 {
			return fmt.Errorf("использование: /newgroup <название>")
		}
		conn.Send(protocol.TypeCreateGroup, protocol.CreateGroup{Name: parts[1]})

	case "/gmsg":
		// /gmsg <group> <text>
		if len(parts) < 3 {
			return fmt.Errorf("использование: /gmsg <группа> <текст>")
		}
		conn.Send(protocol.TypeGroupMsg, protocol.GroupMsg{
			Group:   parts[1],
			Content: parts[2],
		})

	case "/ghistory":
		if len(parts) < 2 {
			return fmt.Errorf("использование: /ghistory <группа>")
		}
		conn.Send(protocol.TypeHistoryReq, protocol.HistoryReq{
			WithGroup: parts[1],
			Limit:     50,
		})

	case "/help":
		printHelp()

	case "/quit", "/exit":
		os.Exit(0)

	default:
		return fmt.Errorf("неизвестная команда %q — /help для списка команд", cmd)
	}
	return nil
}

func handleIncoming(msg client.Incoming, cache *client.MessageCache, me string) {
	switch msg.Type {
	case protocol.TypeRecvMsg:
		var m protocol.RecvMsg
		if err := json.Unmarshal(msg.Payload, &m); err != nil {
			return
		}
		cache.Add(m)
		if m.ReplyToID > 0 {
			fmt.Printf("\n[↩ #%d] %s → %s: %s\n> ", m.ReplyToID, m.FromUser, m.ToUser, m.Content)
		} else {
			fmt.Printf("\n%s → %s: %s\n> ", m.FromUser, m.ToUser, m.Content)
		}

	case protocol.TypeGroupMsg:
		var m protocol.GroupMsg
		if err := json.Unmarshal(msg.Payload, &m); err != nil {
			return
		}
		fmt.Printf("\n[#%s] %s: %s\n> ", m.Group, m.FromUser, m.Content)

	case protocol.TypeHistoryResp:
		var resp protocol.HistoryResp
		if err := json.Unmarshal(msg.Payload, &resp); err != nil {
			return
		}
		fmt.Println("\n--- история ---")
		for _, m := range resp.Messages {
			prefix := ""
			if m.ReplyToID > 0 {
				prefix = fmt.Sprintf("[↩ #%d] ", m.ReplyToID)
			}
			fmt.Printf("  [%s] %s%s: %s\n", m.SentAt, prefix, m.FromUser, m.Content)
		}
		fmt.Print("---------------\n> ")

	case protocol.TypeUserListResp:
		var resp protocol.UserListResp
		if err := json.Unmarshal(msg.Payload, &resp); err != nil {
			return
		}
		fmt.Printf("\nПользователи: %s\n> ", strings.Join(resp.Users, ", "))

	case protocol.TypeServerShutdown:
		var s protocol.ServerShutdown
		json.Unmarshal(msg.Payload, &s) //nolint:errcheck
		fmt.Printf("\n*** %s ***\n", s.Reason)
		os.Exit(0)

	case protocol.TypeError:
		var e protocol.ErrorMsg
		json.Unmarshal(msg.Payload, &e) //nolint:errcheck
		fmt.Printf("\nОшибка сервера: %s\n> ", e.Message)
	}
}

func printHelp() {
	fmt.Println(`Команды:
  /msg <user> <текст>      — личное сообщение
  /reply <id> <текст>      — ответ на сообщение
  /history <user>          — история переписки
  /users                   — список пользователей
  /newgroup <название>     — создать группу
  /gmsg <группа> <текст>   — сообщение в группу
  /ghistory <группа>       — история группы
  /quit                    — выход`)
}
