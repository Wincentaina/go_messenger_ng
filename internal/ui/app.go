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
	"log"
	"strings"
	"time"

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

	// replyTo holds the message being replied to (zero value = no reply).
	replyTo protocol.RecvMsg
	// replyMap caches original messages by ID so formatMessage can show the
	// quote even if the message has been evicted from the conversation cache.
	replyMap map[int64]protocol.RecvMsg

	// typingBar shows "X печатает..." below the chat; auto-clears via typingTimer.
	typingBar   *tview.TextView
	typingTimer *time.Timer

	// onlineSet tracks currently connected users for DM title status display.
	onlineSet map[string]bool

	// lastTypingSent throttles outgoing TypeTyping notifications (max 1 per 2s).
	lastTypingSent time.Time

	// pendingUnread holds chats that received a message before the user list
	// was populated — applied in rebuildUserList when the items exist.
	pendingUnread map[string]bool

	// root layout — needed to attach modal dialogs
	root *tview.Flex
}

// New creates the App but doesn't run it yet.
func New(conn *client.Conn, me string, cache *client.MessageCache) *App {
	a := &App{
		tapp:          tview.NewApplication(),
		conn:          conn,
		me:            me,
		cache:         cache,
		replyMap:      make(map[int64]protocol.RecvMsg),
		onlineSet:     make(map[string]bool),
		pendingUnread: make(map[string]bool),
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
		SetText(fmt.Sprintf("[yellow]go-messenger[-]  вы: [green]%s[-]  |  Tab — панель  |  Ctrl+N — группа  |  Ctrl+A — добавить в группу  |  Ctrl+L — выйти из группы  |  Ctrl+R — ответить  |  Ctrl+D — удалить аккаунт  |  Ctrl+C — выход", a.me))

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

	// Typing indicator bar — 1 row, no border, cleared automatically.
	a.typingBar = tview.NewTextView().SetDynamicColors(true)

	// Bottom: message input.
	a.inputField = tview.NewInputField().
		SetLabel(" > ").
		SetLabelColor(tcell.ColorGreen).
		SetFieldBackgroundColor(tcell.ColorBlack)
	a.inputField.SetBorder(true)

	// Send TypeTyping when the user is actively composing (throttled to 1 per 2s).
	a.inputField.SetChangedFunc(func(text string) {
		if a.currentChat == "" || text == "" {
			return
		}
		if time.Since(a.lastTypingSent) < 2*time.Second {
			return
		}
		a.lastTypingSent = time.Now()
		notif := protocol.TypingNotif{}
		if a.isGroup {
			notif.ToGroup = strings.TrimPrefix(a.currentChat, "#")
		} else {
			notif.ToUser = a.currentChat
		}
		a.conn.Send(protocol.TypeTyping, notif)
	})
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
		if event.Key() == tcell.KeyEsc {
			a.clearReply()
			return nil
		}
		// Ctrl+R opens message picker for reply
		if event.Key() == tcell.KeyCtrlR {
			a.showReplyPicker()
			return nil
		}
		// Ctrl+A — add user to current group
		if event.Key() == tcell.KeyCtrlA {
			a.showAddToGroupDialog()
			return nil
		}
		// Ctrl+L — leave the current group chat
		if event.Key() == tcell.KeyCtrlL {
			a.leaveCurrentGroup()
			return nil
		}
		// Ctrl+D — delete own account (with confirmation)
		if event.Key() == tcell.KeyCtrlD {
			a.showDeleteAccountDialog()
			return nil
		}
		return event
	})

	mainFlex := tview.NewFlex().
		AddItem(a.userList, 22, 0, true).
		AddItem(a.chatView, 0, 1, false)

	a.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.titleBar, 1, 0, false).
		AddItem(mainFlex, 0, 1, true).
		AddItem(a.typingBar, 1, 0, false).
		AddItem(a.inputField, 3, 0, false)

	a.tapp.SetRoot(a.root, true).SetFocus(a.userList)

	// Ctrl+N — create new group
	a.tapp.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlN {
			a.showNewGroupDialog()
			return nil
		}
		return event
	})
}

// openChat switches to a conversation. Called only on Enter (SetSelectedFunc).
func (a *App) openChat(target string) {
	a.currentChat = target
	a.isGroup = strings.HasPrefix(target, "#")
	a.updateChatTitle()
	a.typingBar.SetText("") // clear stale typing indicator from previous chat
	if a.typingTimer != nil {
		a.typingTimer.Stop()
	}

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
	// Force full screen redraw — needed on Windows where tview may not
	// repaint all panels after switching chats.
	a.tapp.Sync()
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

	replyID := a.replyTo.ID
	a.clearReply()

	if a.isGroup {
		a.conn.Send(protocol.TypeGroupMsg, protocol.GroupMsg{
			Group:     strings.TrimPrefix(a.currentChat, "#"),
			Content:   text,
			ReplyToID: replyID,
		})
	} else {
		a.conn.Send(protocol.TypeSendMsg, protocol.SendMsg{
			ToUser:    a.currentChat,
			Content:   text,
			ReplyToID: replyID,
		})
	}
}

// showReplyPicker shows a modal list of recent messages to pick one for reply.
func (a *App) showReplyPicker() {
	var msgs []protocol.RecvMsg
	if a.isGroup {
		msgs = a.cache.GetGroup(strings.TrimPrefix(a.currentChat, "#"))
	} else {
		msgs = a.cache.Get(a.me, a.currentChat)
	}
	if len(msgs) == 0 {
		return
	}

	// Show up to 10 most recent messages, newest at bottom → reverse for display
	start := 0
	if len(msgs) > 10 {
		start = len(msgs) - 10
	}
	recent := msgs[start:]

	list := tview.NewList().ShowSecondaryText(false).SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDarkBlue)
	list.SetBorder(true).SetTitle(" Выберите сообщение для ответа (Enter / Esc) ")

	for _, m := range recent {
		label := m.FromUser + ": " + m.Content
		if len([]rune(label)) > 60 {
			label = string([]rune(label)[:60]) + "…"
		}
		captured := m
		list.AddItem(label, "", 0, func() {
			a.setReplyTo(captured)
			a.tapp.SetRoot(a.root, true).SetFocus(a.inputField)
		})
	}
	// Start with last item selected (most recent)
	list.SetCurrentItem(list.GetItemCount() - 1)

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc {
			a.tapp.SetRoot(a.root, true).SetFocus(a.inputField)
			return nil
		}
		return event
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(list, 14, 0, true).
			AddItem(nil, 0, 1, false), 0, 2, true).
		AddItem(nil, 0, 1, false)

	a.tapp.SetRoot(modal, true).SetFocus(list)
}

// setReplyTo stores the chosen message and updates the input label.
func (a *App) setReplyTo(m protocol.RecvMsg) {
	a.replyTo = m
	if m.ID > 0 {
		a.replyMap[m.ID] = m // persist for quote rendering after clearReply
	}
	preview := m.Content
	if len([]rune(preview)) > 30 {
		preview = string([]rune(preview)[:30]) + "…"
	}
	a.inputField.SetLabel(fmt.Sprintf(" ↩ %s: %s > ", m.FromUser, preview))
}

// clearReply resets the reply state and restores the normal input label.
func (a *App) clearReply() {
	a.replyTo = protocol.RecvMsg{}
	a.inputField.SetLabel(" > ")
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
		log.Printf("ui: TypeGroupMsg id=%d group=%q from=%q replyToID=%d", m.ID, m.Group, m.FromUser, m.ReplyToID)
		recv := protocol.RecvMsg{
			ID:        m.ID,
			FromUser:  m.FromUser,
			ToGroup:   m.Group,
			Content:   m.Content,
			SentAt:    m.SentAt,
			ReplyToID: m.ReplyToID,
		}
		a.cache.Add(recv)
		if recv.ID > 0 {
			a.replyMap[recv.ID] = recv
		}

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
			// Populate replyMap so quote display works for all history messages
			if m.ID > 0 {
				a.replyMap[m.ID] = m
				log.Printf("ui: history add to replyMap id=%d from=%q replyToID=%d", m.ID, m.FromUser, m.ReplyToID)
			}
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
			a.rebuildUserList(resp.Users, resp.Online, resp.Groups)
		})

	case protocol.TypeTyping:
		var notif protocol.TypingNotif
		if err := json.Unmarshal(msg.Payload, &notif); err != nil {
			return
		}
		// Only show if the notification is for the currently open chat
		relevant := !a.isGroup && notif.FromUser == a.currentChat
		if !relevant && a.isGroup {
			relevant = "#"+notif.ToGroup == a.currentChat
		}
		if !relevant {
			return
		}
		a.tapp.QueueUpdateDraw(func() {
			a.typingBar.SetText(fmt.Sprintf(" [grey]%s печатает...[-]", notif.FromUser))
			if a.typingTimer != nil {
				a.typingTimer.Stop()
			}
			a.typingTimer = time.AfterFunc(3*time.Second, func() {
				a.tapp.QueueUpdateDraw(func() { a.typingBar.SetText("") })
			})
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
	// System message (e.g. "user X invited by Y") — no sender, displayed as a divider
	if m.FromUser == "" {
		return fmt.Sprintf("[grey]── %s ──[-]\n", m.Content)
	}

	ts := ""
	if len(m.SentAt) >= 16 {
		ts = m.SentAt[11:16]
	}
	nameColor := "[cyan]"
	if m.FromUser == a.me {
		nameColor = "[green]"
	}
	if m.ReplyToID > 0 {
		quote := ""
		if orig := a.findMessageByID(m.ReplyToID); orig != nil {
			preview := orig.Content
			if len([]rune(preview)) > 40 {
				preview = string([]rune(preview)[:40]) + "…"
			}
			quote = fmt.Sprintf("[grey]  ┌ %s: %s[-]\n", orig.FromUser, preview)
		}
		return fmt.Sprintf("%s[grey]%s[-] %s%s[-]: %s\n", quote, ts, nameColor, m.FromUser, m.Content)
	}
	return fmt.Sprintf("[grey]%s[-] %s%s[-]: %s\n",
		ts, nameColor, m.FromUser, m.Content)
}

// findMessageByID looks up a message by ID, checking replyMap first (always
// populated at reply-selection time), then falling back to the conversation cache.
func (a *App) findMessageByID(id int64) *protocol.RecvMsg {
	if m, ok := a.replyMap[id]; ok {
		log.Printf("ui: findMessageByID id=%d found in replyMap", id)
		return &m
	}
	var msgs []protocol.RecvMsg
	if a.isGroup {
		msgs = a.cache.GetGroup(strings.TrimPrefix(a.currentChat, "#"))
	} else {
		msgs = a.cache.Get(a.me, a.currentChat)
	}
	for i := range msgs {
		if msgs[i].ID == id {
			log.Printf("ui: findMessageByID id=%d found in cache", id)
			return &msgs[i]
		}
	}
	log.Printf("ui: findMessageByID id=%d NOT FOUND (replyMap size=%d, cache size=%d)", id, len(a.replyMap), len(msgs))
	return nil
}

// showAddToGroupDialog opens a modal to add a user to the current group.
func (a *App) showAddToGroupDialog() {
	if !a.isGroup {
		a.setStatus("[red]Ctrl+A работает только в группах[-]")
		return
	}
	groupName := strings.TrimPrefix(a.currentChat, "#")

	var form *tview.Form
	form = tview.NewForm().
		AddInputField("Имя пользователя", "", 24, nil, nil).
		AddButton("Добавить", func() {
			username := strings.TrimSpace(form.GetFormItemByLabel("Имя пользователя").(*tview.InputField).GetText())
			if username != "" {
				a.conn.Send(protocol.TypeAddToGroup, protocol.AddToGroup{Group: groupName, User: username})
			}
			a.tapp.SetRoot(a.root, true).SetFocus(a.inputField)
		}).
		AddButton("Отмена", func() {
			a.tapp.SetRoot(a.root, true).SetFocus(a.inputField)
		})
	form.SetBorder(true).SetTitle(fmt.Sprintf(" Добавить в #%s ", groupName))

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 9, 0, true).
			AddItem(nil, 0, 1, false), 44, 0, true).
		AddItem(nil, 0, 1, false)

	a.tapp.SetRoot(modal, true).SetFocus(form)
}

// leaveCurrentGroup shows a confirmation modal before leaving the current group.
func (a *App) leaveCurrentGroup() {
	if !a.isGroup {
		a.setStatus("[red]Ctrl+L работает только в группах[-]")
		return
	}
	groupName := strings.TrimPrefix(a.currentChat, "#")
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Покинуть группу [yellow]#%s[-]?", groupName)).
		AddButtons([]string{"Покинуть", "Отмена"}).
		SetDoneFunc(func(_ int, label string) {
			if label == "Покинуть" {
				a.conn.Send(protocol.TypeLeaveGroup, protocol.LeaveGroup{Group: groupName})
				a.currentChat = ""
				a.isGroup = false
				a.chatView.SetTitle(" Чат ")
				a.chatView.SetText("")
				a.tapp.SetRoot(a.root, true).SetFocus(a.userList)
				a.setStatus(fmt.Sprintf("[yellow]Вы покинули группу #%s[-]", groupName))
			} else {
				a.tapp.SetRoot(a.root, true).SetFocus(a.inputField)
			}
		})
	a.tapp.SetRoot(modal, true).SetFocus(modal)
}

// showDeleteAccountDialog shows a confirmation modal before soft-deleting the account.
func (a *App) showDeleteAccountDialog() {
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Удалить аккаунт [red]%s[-]?\nЭто действие нельзя отменить.\nВаши сообщения останутся как 'deleted_user'.", a.me)).
		AddButtons([]string{"Удалить", "Отмена"}).
		SetDoneFunc(func(_ int, label string) {
			if label == "Удалить" {
				a.conn.Send(protocol.TypeDeleteAccount, protocol.DeleteAccountReq{})
				a.tapp.Stop()
			} else {
				a.tapp.SetRoot(a.root, true).SetFocus(a.inputField)
			}
		})
	a.tapp.SetRoot(modal, true).SetFocus(modal)
}

// showNewGroupDialog opens a modal input for creating a new group.
func (a *App) showNewGroupDialog() {
	var form *tview.Form
	form = tview.NewForm().
		AddInputField("Название", "", 20, nil, nil).
		AddInputField("Участники (через ,)", "", 20, nil, nil).
		AddButton("Создать", func() {
			name := form.GetFormItemByLabel("Название").(*tview.InputField).GetText()
			membersRaw := form.GetFormItemByLabel("Участники (через ,)").(*tview.InputField).GetText()

			var members []string
			for _, m := range strings.Split(membersRaw, ",") {
				m = strings.TrimSpace(m)
				if m != "" && m != a.me {
					members = append(members, m)
				}
			}

			if name != "" {
				a.conn.Send(protocol.TypeCreateGroup, protocol.CreateGroup{
					Name:    name,
					Members: members,
				})
			}
			a.tapp.SetRoot(a.root, true).SetFocus(a.inputField)
		}).
		AddButton("Отмена", func() {
			a.tapp.SetRoot(a.root, true).SetFocus(a.userList)
		})
	form.SetBorder(true).SetTitle(" Новая группа ")

	// Center the modal — width 44 fits two 20-char fields + labels + padding
	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 11, 0, true).
			AddItem(nil, 0, 1, false), 44, 0, true).
		AddItem(nil, 0, 1, false)

	a.tapp.SetRoot(modal, true).SetFocus(form)
}

// rebuildUserList repopulates the left panel, preserving unread markers.
// Online users are shown in green; offline users in the default colour.
func (a *App) rebuildUserList(users []string, online []string, groups []string) {
	onlineSet := make(map[string]bool, len(online))
	for _, u := range online {
		onlineSet[u] = true
	}
	a.onlineSet = onlineSet
	a.updateChatTitle()

	// Preserve unread markers across rebuild
	unread := make(map[string]bool)
	for i := 0; i < a.userList.GetItemCount(); i++ {
		main, _ := a.userList.GetItemText(i)
		if strings.HasPrefix(main, "● ") {
			unread[stripLabel(main)] = true
		}
	}

	a.userList.Clear()

	// Direct message contacts (skip self)
	for _, u := range users {
		if u == a.me || u == "" {
			continue
		}
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

	// Group chats (shown with # prefix in grey)
	for _, g := range groups {
		key := "#" + g
		dot := ""
		if unread[key] {
			dot = "● "
		}
		a.userList.AddItem(fmt.Sprintf("%s[yellow]#%s[-]", dot, g), "", 0, nil)
	}

	// Apply unread markers that arrived before the list was populated
	for target := range a.pendingUnread {
		a.markUnread(target)
	}
	a.pendingUnread = make(map[string]bool)
}

// markUnread adds a "●" indicator to show there's a new unread message.
// If the target is not yet in the list (UserListResp hasn't arrived),
// it is stored in pendingUnread and applied in rebuildUserList.
func (a *App) markUnread(target string) {
	found := false
	for i := 0; i < a.userList.GetItemCount(); i++ {
		main, _ := a.userList.GetItemText(i)
		if stripLabel(main) == target {
			found = true
			if !strings.HasPrefix(main, "● ") {
				a.userList.SetItemText(i, "● "+main, "")
			}
		}
	}
	if !found {
		a.pendingUnread[target] = true
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

// updateChatTitle refreshes the chat panel title with online status for DMs.
func (a *App) updateChatTitle() {
	if a.currentChat == "" {
		a.chatView.SetTitle(" Чат ")
		return
	}
	if a.isGroup {
		a.chatView.SetTitle(fmt.Sprintf(" Чат с: %s ", a.currentChat))
		return
	}
	status := "[grey](офлайн)[-]"
	if a.onlineSet[a.currentChat] {
		status = "[green](онлайн)[-]"
	}
	a.chatView.SetTitle(fmt.Sprintf(" Чат с: %s %s ", a.currentChat, status))
}

func (a *App) setStatus(msg string) {
	a.titleBar.SetText(fmt.Sprintf(
		"[yellow]go-messenger[-]  вы: [green]%s[-]  |  %s", a.me, msg,
	))
}
