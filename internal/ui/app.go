// Package ui implements the terminal user interface using tview.
//
// Layout:
//
//	┌─────────────────────────────────────────────────────┐
//	│  go-messenger  [вы: alice]                          │
//	├──────────────┬──────────────────────────────────────┤
//	│ Собеседники  │ Чат с: bob                           │
//	│              │                                      │
//	│ > bob        │ [10:23] alice: Привет!               │
//	│   oleg       │ [10:23] bob: Привет!                 │
//	│   #dev       │                                      │
//	├──────────────┴──────────────────────────────────────┤
//	│ > Введите сообщение...                              │
//	└─────────────────────────────────────────────────────┘
package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/wincentaina/go_messenger_ng/internal/client"
	"github.com/wincentaina/go_messenger_ng/internal/protocol"
)

// App is the main TUI application.
type App struct {
	tapp  *tview.Application
	conn  *client.Conn
	me    string
	cache *client.MessageCache

	// UI widgets
	userList   *tview.List
	chatView   *tview.TextView
	inputField *tview.InputField
	titleBar   *tview.TextView

	// currentChat is the open conversation: username or "#groupname".
	currentChat string
	isGroup     bool

	// pendingHistoryFor tracks which chat the last HistoryReq was for,
	// so we don't overwrite the current view with a stale response.
	pendingHistoryFor string
}

// New creates the App but doesn't run it yet.
func New(conn *client.Conn, me string, cache *client.MessageCache) *App {
	a := &App{
		tapp:  tview.NewApplication(),
		conn:  conn,
		me:    me,
		cache: cache,
	}
	a.buildLayout()
	return a
}

// Run starts the event loop; blocks until the user quits.
func (a *App) Run() error {
	a.conn.Send(protocol.TypeUserListReq, protocol.UserListReq{})
	go a.listenIncoming()
	return a.tapp.Run()
}

// buildLayout constructs the tview widget tree.
func (a *App) buildLayout() {
	a.titleBar = tview.NewTextView().
		SetDynamicColors(true).
		SetText(fmt.Sprintf("[yellow]go-messenger[-]  вы: [green]%s[-]  |  Tab — сменить панель  |  Ctrl+C — выход", a.me))

	// Left panel: list of users/groups.
	// SetChangedFunc fires on arrow-key navigation — only updates the title.
	// SetSelectedFunc fires on Enter — actually opens the chat.
	a.userList = tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDarkBlue).
		SetMainTextStyle(tcell.StyleDefault) // allows tview colour tags in item labels
	a.userList.SetBorder(true).SetTitle(" Собеседники ")

	a.userList.SetChangedFunc(func(_ int, main, _ string, _ rune) {
		target := stripLabel(main)
		a.chatView.SetTitle(fmt.Sprintf(" Чат с: %s (Enter чтобы открыть) ", target))
	})
	a.userList.SetSelectedFunc(func(_ int, main, _ string, _ rune) {
		target := stripLabel(main)
		a.openChat(target)
	})

	// Right panel: scrollable chat history.
	// No SetChangedFunc — we use SetText for batch updates to avoid flicker.
	a.chatView = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true)
	a.chatView.SetBorder(true).SetTitle(" Чат ")

	// Bottom: message input.
	a.inputField = tview.NewInputField().
		SetLabel(" > ").
		SetLabelColor(tcell.ColorGreen).
		SetFieldBackgroundColor(tcell.ColorBlack)
	a.inputField.SetBorder(true)
	a.inputField.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			a.sendCurrentInput()
		}
	})

	// Tab toggles focus between user list and input field.
	a.userList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab {
			a.tapp.SetFocus(a.inputField)
			return nil
		}
		return event
	})
	a.inputField.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab {
			a.tapp.SetFocus(a.userList)
			return nil
		}
		return event
	})

	mainFlex := tview.NewFlex().
		AddItem(a.userList, 22, 0, true).
		AddItem(a.chatView, 0, 1, false)

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.titleBar, 1, 0, false).
		AddItem(mainFlex, 0, 1, true).
		AddItem(a.inputField, 3, 0, false)

	a.tapp.SetRoot(root, true).SetFocus(a.userList)
}

// openChat switches to a conversation. Called only on Enter (SetSelectedFunc).
func (a *App) openChat(target string) {
	a.currentChat = target
	a.isGroup = strings.HasPrefix(target, "#")
	a.chatView.SetTitle(fmt.Sprintf(" Чат с: %s ", target))

	// Clear unread marker in the list (keep colour tag intact)
	for i := 0; i < a.userList.GetItemCount(); i++ {
		main, _ := a.userList.GetItemText(i)
		if stripLabel(main) == target && strings.HasPrefix(main, "● ") {
			a.userList.SetItemText(i, strings.TrimPrefix(main, "● "), "")
		}
	}

	// Render cached messages immediately (no flicker: single SetText call)
	var msgs []protocol.RecvMsg
	if a.isGroup {
		msgs = a.cache.GetGroup(strings.TrimPrefix(target, "#"))
	} else {
		msgs = a.cache.Get(a.me, target)
	}
	a.renderMessages(msgs)

	// Request fresh history from server
	req := protocol.HistoryReq{Limit: 50}
	if a.isGroup {
		req.WithGroup = strings.TrimPrefix(target, "#")
	} else {
		req.WithUser = target
	}
	a.pendingHistoryFor = target
	a.conn.Send(protocol.TypeHistoryReq, req)

	a.tapp.SetFocus(a.inputField)
}

// sendCurrentInput reads the input field and sends the message.
func (a *App) sendCurrentInput() {
	text := strings.TrimSpace(a.inputField.GetText())
	if text == "" {
		return
	}
	a.inputField.SetText("")

	if a.currentChat == "" {
		a.setStatus("[red]Выберите собеседника (стрелки + Enter)[-]")
		return
	}

	if a.isGroup {
		a.conn.Send(protocol.TypeGroupMsg, protocol.GroupMsg{
			Group:   strings.TrimPrefix(a.currentChat, "#"),
			Content: text,
		})
	} else {
		a.conn.Send(protocol.TypeSendMsg, protocol.SendMsg{
			ToUser:  a.currentChat,
			Content: text,
		})
	}
}

// listenIncoming receives server messages and updates the UI via QueueUpdateDraw.
// All widget updates MUST go through QueueUpdateDraw — never touch tview directly
// from this goroutine.
func (a *App) listenIncoming() {
	for {
		select {
		case msg, ok := <-a.conn.Incoming():
			if !ok {
				return
			}
			a.handleIncoming(msg)
		case <-a.conn.Done():
			a.tapp.QueueUpdateDraw(func() {
				a.setStatus("[red]Соединение с сервером разорвано[-]")
			})
			return
		}
	}
}

func (a *App) handleIncoming(msg client.Incoming) {
	switch msg.Type {

	case protocol.TypeRecvMsg:
		var m protocol.RecvMsg
		if err := json.Unmarshal(msg.Payload, &m); err != nil {
			return
		}
		a.cache.Add(m)

		partner := m.FromUser
		if partner == a.me {
			partner = m.ToUser
		}

		a.tapp.QueueUpdateDraw(func() {
			if partner == a.currentChat {
				// Append to the current view without full re-render
				a.appendMessage(m)
				a.chatView.ScrollToEnd()
			} else {
				a.markUnread(partner)
			}
		})

	case protocol.TypeGroupMsg:
		var m protocol.GroupMsg
		if err := json.Unmarshal(msg.Payload, &m); err != nil {
			return
		}
		recv := protocol.RecvMsg{
			FromUser: m.FromUser,
			ToGroup:  m.Group,
			Content:  m.Content,
			SentAt:   m.SentAt,
		}
		a.cache.Add(recv)

		a.tapp.QueueUpdateDraw(func() {
			if "#"+m.Group == a.currentChat {
				a.appendMessage(recv)
				a.chatView.ScrollToEnd()
			} else {
				a.markUnread("#" + m.Group)
			}
		})

	case protocol.TypeHistoryResp:
		var resp protocol.HistoryResp
		if err := json.Unmarshal(msg.Payload, &resp); err != nil {
			return
		}
		// Only apply if this is still the chat we requested history for.
		// Prevents stale responses from overwriting the current view.
		for _, m := range resp.Messages {
			a.cache.Add(m)
		}
		a.tapp.QueueUpdateDraw(func() {
			if a.pendingHistoryFor == a.currentChat {
				a.renderMessages(resp.Messages)
			}
		})

	case protocol.TypeUserListResp:
		var resp protocol.UserListResp
		if err := json.Unmarshal(msg.Payload, &resp); err != nil {
			return
		}
		a.tapp.QueueUpdateDraw(func() {
			a.rebuildUserList(resp.Users, resp.Online)
		})

	case protocol.TypeServerShutdown:
		var s protocol.ServerShutdown
		json.Unmarshal(msg.Payload, &s) //nolint:errcheck
		a.tapp.QueueUpdateDraw(func() {
			a.setStatus(fmt.Sprintf("[red]%s[-]", s.Reason))
		})

	case protocol.TypeError:
		var e protocol.ErrorMsg
		json.Unmarshal(msg.Payload, &e) //nolint:errcheck
		a.tapp.QueueUpdateDraw(func() {
			a.setStatus(fmt.Sprintf("[red]Ошибка: %s[-]", e.Message))
		})
	}
}

// renderMessages rebuilds the entire chat view from a slice of messages.
// Uses a single SetText call to avoid per-line redraws (no flicker).
func (a *App) renderMessages(msgs []protocol.RecvMsg) {
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(a.formatMessage(m))
	}
	a.chatView.SetText(sb.String())
	a.chatView.ScrollToEnd()
}

// appendMessage adds one line to the chat view (for live incoming messages).
func (a *App) appendMessage(m protocol.RecvMsg) {
	fmt.Fprint(a.chatView, a.formatMessage(m))
}

// formatMessage returns a coloured string for one message.
func (a *App) formatMessage(m protocol.RecvMsg) string {
	ts := ""
	if len(m.SentAt) >= 16 {
		ts = m.SentAt[11:16]
	}
	nameColor := "[cyan]"
	if m.FromUser == a.me {
		nameColor = "[green]"
	}
	if m.ReplyToID > 0 {
		return fmt.Sprintf("[grey]%s[-] %s%s[-] [grey]↩#%d[-]: %s\n",
			ts, nameColor, m.FromUser, m.ReplyToID, m.Content)
	}
	return fmt.Sprintf("[grey]%s[-] %s%s[-]: %s\n",
		ts, nameColor, m.FromUser, m.Content)
}

// rebuildUserList repopulates the left panel, preserving unread markers.
// Online users are shown in green; offline users in the default colour.
func (a *App) rebuildUserList(users []string, online []string) {
	// Index who is online and who has unread messages
	onlineSet := make(map[string]bool, len(online))
	for _, u := range online {
		onlineSet[u] = true
	}

	unread := make(map[string]bool)
	for i := 0; i < a.userList.GetItemCount(); i++ {
		main, _ := a.userList.GetItemText(i)
		plain := stripLabel(main)
		if strings.HasPrefix(main, "● ") {
			unread[plain] = true
		}
	}

	a.userList.Clear()
	for _, u := range users {
		if u == a.me {
			continue
		}

		// Build label: unread dot + colour based on online status
		dot := ""
		if unread[u] {
			dot = "● "
		}

		var label string
		if onlineSet[u] {
			label = fmt.Sprintf("%s[green]%s[-]", dot, u)
		} else {
			label = fmt.Sprintf("%s[white]%s[-]", dot, u)
		}
		a.userList.AddItem(label, "", 0, nil)
	}
}

// markUnread adds a "●" indicator to show there's a new unread message.
func (a *App) markUnread(target string) {
	for i := 0; i < a.userList.GetItemCount(); i++ {
		main, _ := a.userList.GetItemText(i)
		if stripLabel(main) == target && !strings.HasPrefix(main, "● ") {
			a.userList.SetItemText(i, "● "+main, "")
		}
	}
}

// stripLabel removes the unread dot and tview colour tags from a list label,
// returning the plain username.
func stripLabel(label string) string {
	s := strings.TrimPrefix(label, "● ")
	// Remove tview colour tags like [green] and [-]
	for strings.Contains(s, "[") {
		open := strings.Index(s, "[")
		close := strings.Index(s, "]")
		if close > open {
			s = s[:open] + s[close+1:]
		} else {
			break
		}
	}
	return s
}

func (a *App) setStatus(msg string) {
	a.titleBar.SetText(fmt.Sprintf(
		"[yellow]go-messenger[-]  вы: [green]%s[-]  |  %s", a.me, msg,
	))
}
