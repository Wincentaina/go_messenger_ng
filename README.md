# go_messenger_ng

Мессенджер клиент-сервер на Go. Курсовая работа по дисциплине «Операционные системы».

## Стек

| Компонент | Технология |
|-----------|-----------|
| Язык | Go 1.22 |
| База данных | PostgreSQL 16 (Docker) |
| TUI клиента | [tview](https://github.com/rivo/tview) |
| Транспорт | TCP + TLS 1.3 (самоподписанный сертификат) |
| Инфраструктура | Docker Compose |

## Архитектура

```
┌─────────────────────┐  TCP+TLS   ┌────────────────────────────────┐
│       Client        │◄──────────►│            Server              │
│                     │            │                                │
│  goroutine: reader  │            │  acceptLoop goroutine          │
│  goroutine: writer  │            │  hub goroutine (маршрутизация) │
│  TUI (tview)        │            │  per-client goroutine (I/O)    │
│  MessageCache       │            │                                │
└─────────────────────┘            │  PostgreSQL                    │
                                   │  users, messages, groups, logs │
                                   └────────────────────────────────┘
```

### Hub — центральный маршрутизатор

Единственная горутина, владеющая картой подключённых клиентов. Все входящие сообщения попадают в канал `route`, hub читает его и раздаёт адресатам без гонок данных (mutex не нужен для самой карты).

### Бинарный протокол

```
┌──────────┬──────────┬──────────────┬─────────────────┐
│ 2 bytes  │  1 byte  │   4 bytes    │    N bytes      │
│  magic   │ msg_type │    length    │  JSON payload   │
│ 0xAB 0xCD│  (enum)  │  big-endian  │                 │
└──────────┴──────────┴──────────────┴─────────────────┘
  Итого заголовок: 7 байт
```

Magic bytes `0xAB 0xCD` позволяют однозначно идентифицировать протокол в Wireshark.

| Тип | Значение | Направление |
|-----|----------|-------------|
| `auth_req` | 0x01 | C→S |
| `auth_resp` | 0x02 | S→C |
| `send_msg` | 0x03 | C→S |
| `recv_msg` | 0x04 | S→C |
| `history_req` | 0x05 | C→S |
| `history_resp` | 0x06 | S→C |
| `user_list_req` | 0x07 | C→S |
| `user_list_resp` | 0x08 | S→C |
| `create_group` | 0x09 | C→S |
| `group_msg` | 0x0A | C↔S |
| `error` | 0x0B | S→C |
| `server_shutdown` | 0x0C | S→C |

### BST для списка пользователей

Зарегистрированные пользователи хранятся в потокобезопасном BST (`internal/util/bst.go`). Вставка O(log n), обход в алфавитном порядке O(n) — без дополнительного запроса к БД при каждом `user_list_req`.

## Реализованные бонусы

| Бонус | Описание |
|-------|----------|
| Bonus 1 | Многопоточность — горутины на каждого клиента + hub |
| Bonus 2 | Групповые чаты (создание, сообщения, история) |
| Bonus 3a | Ответ на сообщение (reply) |
| Bonus 4 | Хранение истории в PostgreSQL |
| Bonus 5 | Постраничный запрос истории (`history_req/resp`) |
| Bonus 6 | TLS шифрование (TLS 1.3, самоподписанный сертификат с SAN) |
| Bonus 7a | Кросс-платформенная компиляция Linux/macOS |
| Bonus 8 | BST для индекса пользователей |
| Bonus 9 | Обработка UNIX-сигналов: SIGTERM/SIGINT → graceful shutdown, SIGHUP → reload |
| Bonus 10 | Полная поддержка UTF-8 (русский язык в сообщениях и TUI) |

## Быстрый старт

### 1. Сертификаты

```bash
make certs
```

### 2. Поднять PostgreSQL

```bash
make docker-up
```

### 3. Запустить сервер

```bash
make run-server
```

### 4. Запустить клиент (в другом терминале)

```bash
make run-client
```

При первом запуске введите имя пользователя и пароль, на вопрос «Регистрация? (y/N)» ответьте `y`.

## Управление TUI

| Клавиша | Действие |
|---------|----------|
| `↑ / ↓` | Навигация по списку собеседников |
| `Enter` | Открыть чат |
| `Tab` | Переключить фокус список ↔ ввод |
| `Ctrl+N` | Создать новую группу |
| `Ctrl+R` | Ответить на последнее сообщение |
| `Esc` | Отменить ответ |
| `Ctrl+C` | Выйти |

## Структура проекта

```
go_messenger_ng/
├── cmd/
│   ├── server/main.go          # точка входа сервера
│   └── client/main.go          # точка входа клиента
├── internal/
│   ├── protocol/               # типы сообщений, Encode/Decode
│   ├── server/                 # hub, обработчик клиентов, auth
│   ├── client/                 # соединение, кэш сообщений
│   ├── db/                     # PostgreSQL запросы
│   ├── crypto/                 # загрузка TLS сертификатов
│   ├── ui/                     # tview TUI
│   └── util/                   # BST
├── migrations/001_init.sql     # схема БД
├── config/                     # server.yaml, client.yaml
├── scripts/gen_certs.sh        # генерация самоподписанного сертификата
├── docker-compose.yml
└── Makefile
```

## Сборка

```bash
# Обе бинарки в ./bin/
make build

# Тесты
make test

# Cross-compile для Linux
GOOS=linux GOARCH=amd64 go build -o bin/server-linux ./cmd/server
GOOS=linux GOARCH=amd64 go build -o bin/client-linux ./cmd/client
```
