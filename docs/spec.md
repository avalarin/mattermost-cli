# mattermost-cli — Спецификация проекта

> Консольный TUI-клиент для Mattermost на Go с встроенным AI-агентом.
> Версия: 0.3 | Дата: 2026-05-17

---

## 1. Цель проекта

Создать терминальный клиент для Mattermost, который:
- Позволяет читать и отправлять сообщения из терминала без браузера.
- Отображает каналы, треды, реакции и непрочитанные сообщения.
- Содержит встроенный AI-агент (Claude), который видит входящие сообщения, может открывать каналы, читать историю и отправлять сообщения от имени пользователя.

**Целевая аудитория**: разработчики и power-users, работающие в терминале большую часть дня.

---

## 2. Технический стек

| Слой | Технология | Обоснование |
|---|---|---|
| Язык | Go 1.24+ | Один бинарь, встроенный tooling, без runtime зависимостей |
| TUI | [Bubble Tea](https://github.com/charmbracelet/bubbletea) | Elm Architecture, активное сообщество, Lip Gloss + Bubbles |
| Стилизация | Lip Gloss | Адаптивные цвета, border-boxes, flex-like layout |
| Mattermost API | кастомный REST/WS клиент (`internal/mattermost`) | Минимальный набор эндпоинтов, полный контроль над WS-соединением |
| WebSocket | `github.com/coder/websocket` | Fork nhooyr.io/websocket, pure Go, активно поддерживается |
| AI | `github.com/anthropics/anthropic-sdk-go` | Claude API, tool_use, streaming |
| БД | SQLite (`modernc.org/sqlite`) | Pure Go, без CGO, хранит сообщения + AI-историю |
| Конфиг | TOML (`github.com/BurntSushi/toml`) | Читаемый формат, стандарт для Go-утилит |
| Логи | `log/slog` | Встроен в stdlib с Go 1.21, структурированный |

---

## 3. Конфигурация

**Путь (продакшен)**: `~/.config/mattermost-cli/config.toml`  
**Путь (разработка)**: `config.dev.toml` в корне проекта (gitignored); шаблон — `config.example.toml`

```toml
[server]
url   = "https://mattermost.example.com"
token = "your-personal-access-token"
team  = "my-team"           # slug команды

[ai]
api_key = ""                # или переменная ANTHROPIC_API_KEY
model   = "claude-sonnet-4-6"
enabled = false

[ui]
date_format          = "15:04"       # или "2006-01-02 15:04"
message_limit        = 100           # сколько сообщений грузить при открытии канала
theme                = "auto"        # "auto" | "dark" | "light"
channels_width       = 22            # ширина боковой панели каналов (символов)
channel_messages     = "root_only"   # "root_only" | "all"
show_mode_indicator  = true          # показывать индикатор режима в статус-баре

[channels]
sort          = "alphabetical"   # "alphabetical" | "last_message"
unread_only   = false            # если true — скрывать каналы без непрочитанных
archived_only = false            # если true — показывать только архивированные каналы

[colors]
active_header_bg = "237"         # фон заголовка активной панели; ANSI 256 или #RRGGBB
active_header_fg = "15"          # текст заголовка активной панели

[debug]
# enabled = false                # включает структурированные логи в debug.log
```

Переменные окружения `MATTERMOST_URL`, `MATTERMOST_TOKEN`, `ANTHROPIC_API_KEY` перекрывают значения из файла.

---

## 4. Локальное хранилище

```
~/.config/mattermost-cli/
├── config.toml      # настройки
├── db.sqlite        # кэш сообщений, каналов, AI-история, watched-каналы
└── debug.log        # отладочные логи (только при --debug)
```

**Таблицы SQLite:**

| Таблица | Содержимое |
|---|---|
| `channels` | id, name, display_name, type, unread_count |
| `messages` | id, channel_id, user_id, text, create_at, root_id |
| `ai_history` | id, session_id, role, content, tool_calls, created_at |
| `watched_channels` | channel_id — каналы за которыми следит AI |

При старте приложение показывает кэшированные сообщения из `messages` и подключается к WS; новые события дописываются в БД.

---

## 5. Вехи (Milestones)

### ✅ M1 — Базовая лента + авторизация + отправка

**Цель**: минимальный рабочий клиент, с которым можно работать.

**Функциональность:**
- Авторизация через Personal Access Token из конфига.
- Подключение к Mattermost WebSocket Events API.
- При старте — загрузка кэша из SQLite, затем обновление через WS.
- Единая лента (`All Activity`) — все входящие сообщения из всех каналов команды в хронологическом порядке.
- Треды отображаются как обычные сообщения с пометкой `↩ В ответ на: <первые N слов родителя>`.
- Команда отправки: `/send #channel-name Текст`. Имя канала проверяется на сервере в момент отправки; если канал не найден — сообщение об ошибке в статус-баре.
- WebSocket reconnect: exponential backoff, потолок 60 сек, jitter ±20%. Статус соединения всегда виден в шапке.

**TUI Layout (M1):**
```
┌──────────────────────────────────────────────────────┐
│ mattermost-cli  [connected]              team: avito │
├──────────────────────────────────────────────────────┤
│ [10:01] #general  john: привет всем                  │
│ [10:02] #backend  alice: PR готов, смотрите          │
│ [10:03] #general  ↩ bob: привет!                     │
│                                                      │
│ ...                                                  │
├──────────────────────────────────────────────────────┤
│ > /send #general Текст...                            │
└──────────────────────────────────────────────────────┘
```

Статусы шапки: `[connected]` → `[reconnecting... 8s]` → `[connected]`.

**Клавиши:**
| Клавиша | Действие |
|---|---|
| `↑` / `↓` | Прокрутка ленты вверх/вниз |
| `PgUp` / `PgDn` | Прокрутка ленты быстро |
| `End` | Прыжок к последнему сообщению |
| `/` | Открыть командную строку |
| `Esc` | Сбросить ввод / отменить действие |
| `Ctrl+C` | Очистить поле ввода (при непустом) / показать подсказку (при пустом) |
| `/quit` | Выход из приложения |

---

### ✅ M2 — Каналы, переключение, непрочитанные, треды

**Функциональность:**
- Левая панель - channels: список каналов с бейджем непрочитанных (`#general (3)`). Счётчик непрочитанных — с сервера при старте, далее поддерживается через WS.
	- `Ctrl+L` — переход в channels
	- Тип канала обозначается префиксом: `#` (публичный/приватный), `@` (DM)
	- Текущий **открытый** канал подсвечивается белым фоном (текст чёрный)
	- Навигация стрелками up/down и page up/page down; текущий **выбранный** канал подсвечивается светло-серым цветом
	- По `Enter` канал **открывается** и показываются его сообщения; просто навигация — только **выбор**, не открытие
	- При навигации есть скроллинг каналов
	- При **открытии** канала обновляется окно messages
	- Канал помечается прочитанным **при уходе из него** (переходе на другой канал)
	- Архивированные каналы видны с маркером `[x]`; открываются как обычные каналы
	- `i` в ModeChannels — popup с информацией о выбранном канале (название, описание, участники)
- Popup - thread_popup: показывает весь тред с полным текстом сообщений, без сокращений
	- thread_popup — overlay поверх channels и messages; input остаётся внизу
	- Открытие по `Enter` из секции messages на выбранном сообщении
	- Навигация ↑/↓ по сообщениям внутри popup
	- Горячие клавиши в popup: `r` — вставляет `/reply` в input и переходит в input; `e` — вставляет `/edit <текст>` (только автор); `d` — заглушка (реализация в M3)
	- При переходе в input thread_popup остаётся открытым
- Главная панель - messages
	- Если открыт «All Activity» — все сообщения, включая ответы в тредах; у ответов бейдж `⤴︎`
	- Если открыт канал — только root-сообщения (при `channel_messages = "root_only"`); у сообщений с ответами бейдж `⤵︎ N`
	- Просмотр треда: `Enter` на сообщении открывает thread_popup
- Popup - search/sort/filter (`Ctrl+K`): объединённый popup поиска каналов/пользователей, сортировки и фильтрации
	- При вводе < 2 символов — показывает все локальные каналы («All Activity» всегда первым)
	- При вводе ≥ 2 символов — REST-поиск: каналы `#` + пользователи `@`
	- Нижняя секция: сортировка (Alphabetical / Last message) и фильтры (Unread only / Archived only); изменения применяются при `Enter`, отменяются по `Esc`
	- `Enter` на канале — открывает его, channels panel подсвечивает его открытым
	- `Enter` на пользователе — открывает DM с ним
	- Закрытие по `Ctrl+C` или `Esc`
	- Внизу popup — список горячих клавиш

**TUI Layout (M2):**
```
┌──────────┬────────────────────────────────────────────┐
│ Channels │ #general                                   │
│          │                                            │
│ #general │ [10:01] john: привет                       │
│   (3)    │ [10:02] alice: смотрите PR                 │
│ #backend │ [10:03] ↩ 3  bob: привет!                  │
│ #random  │                                            │
├──────────┴────────────────────────────────────────────┤
│ > Напишите сообщение...                               │
└───────────────────────────────────────────────────────┘
```

**Клавиши:**
| Клавиша | Действие |
|---|---|
| `Ctrl+B` | Prefix-клавиша (tmux-style): `+↑/→` messages, `+↓` input, `+←` channels |
| `Ctrl+J` | Переход в messages |
| `Ctrl+L` | Переход в channels |
| `Ctrl+K` | Открыть search/sort/filter popup |
| `i` (в channels) | Открыть popup информации о канале |
| `↑` / `↓` | Навигация по каналам / прокрутка сообщений |
| `PgUp` / `PgDn` | Быстрая прокрутка |
| `End` | Прыжок к последнему сообщению |
| `Enter` (в channels) | Открыть выбранный канал |
| `Enter` (в messages) | Открыть thread popup |
| `r` (в thread popup) | Вставить `/reply` в input |
| `e` (в thread popup, автор) | Вставить `/edit <текст>` в input |
| `Esc` | Закрыть popup / вернуться в input |


---

### M3 — Работа с сообщениями

**Функциональность:**
- Реакции: `r` → поле ввода `:emoji_code:` → TUI конвертирует в unicode (`👍`, `🔥` и т.д.). Рядом с сообщением отображаются все реакции с счётчиком.
- Редактирование своего сообщения: `e` → поле ввода с текущим текстом.
- Удаление своего сообщения: `d` → подтверждение `y/n`.
- Копирование текста сообщения в буфер обмена (`pbcopy`, macOS): `y`.
- Команда `/watch #channel` — добавить канал в список "наблюдаемых" AI-агентом (сохраняется в `watched_channels`).
- поиск по сообщениям (search_message)
	- ctrl+f - перехов в search_message (когда будет)

---

### M4 — Расширенные команды

**Функциональность:**
- Создание DM: `/dm @username`.
- Поиск пользователей: `/find @prefix` → автодополнение.
- Создание публичного/приватного канала: `/create-channel name [--private]`.
- Поиск по сообщениям: `/search query` → результаты в отдельной панели.
- OAuth 2.0: открытие браузера + локальный HTTP-сервер для приёма redirect. PAT остаётся как альтернатива.

---

## 6. AI-агент (встроенный режим)

### Концепция

AI-панель (`Ctrl+A`) занимает нижнюю треть экрана. Пользователь общается с Claude в чат-интерфейсе. Claude видит контекст текущего канала и реагирует на сообщения в watched-каналах.

```
┌──────────┬─────────────────────────────────────────┐
│ Channels │ #general                                │
│ #general │ [10:01] john: готов к демо?             │
│ #backend │ [10:02] alice: да, жду                  │
├──────────┴─────────────────────────────────────────┤
│ [AI] #general (watched): john спрашивает про демо. │
│      Ответить?                                     │
│ > нет, скажи что я занят                           │
│ [AI] Отправить в #general: "Буду чуть позже, занят"│
│      Подтвердить? (y/n)                            │
└────────────────────────────────────────────────────┘
```

**Watched-каналы**: `/watch #channel` добавляет канал в список. WS-события из этих каналов проксируются в AI-сессию — агент проактивно пишет в панель при новых сообщениях. `/unwatch #channel` убирает из списка.

**Подтверждение отправки**: любое действие AI, меняющее состояние на сервере (отправка, реакция), требует явного `y/n` от пользователя.

### Инструменты AI (tool_use)

| Инструмент | Описание |
|---|---|
| `list_channels` | Список каналов + unread count + watched-статус |
| `get_messages(channel, limit)` | Последние N сообщений из канала |
| `switch_channel(channel)` | Переключить активный канал в TUI |
| `send_message(channel, text)` | Отправить сообщение (требует подтверждения) |
| `reply_to(message_id, text)` | Ответить в тред (требует подтверждения) |
| `get_thread(message_id)` | Загрузить тред сообщения |
| `react(message_id, emoji)` | Поставить реакцию (требует подтверждения) |
| `summarize_channel(channel, limit)` | Claude суммаризирует последние N сообщений |

### Управление контекстом

- Каждый запрос включает: системный промпт + список каналов + последние 50 сообщений активного канала.
- История AI-диалога: скользящее окно последних 20 пар (user/assistant). При превышении — старые пары отбрасываются.
- История сохраняется в `db.sqlite` (таблица `ai_history`) и восстанавливается при следующем запуске.
- Streaming ответов выводится в AI-панель по мере генерации.
- При `ai.enabled = false` или отсутствии ключа — панель недоступна, остальное работает.

---

## 7. Архитектура приложения

### Структура пакетов

```
mattermost-cli/
├── cmd/
│   └── mattermost-cli/
│       └── main.go                  # точка входа, CLI-флаги (--debug, --config)
├── internal/
│   ├── config/
│   │   └── config.go                # загрузка TOML + env override + валидация
│   ├── mattermost/
│   │   ├── client.go                # REST-методы (send, react, mark_read, search, ...)
│   │   ├── websocket.go             # WS Events API, reconnect, exponential backoff
│   │   └── types.go                 # локальные типы (Message, Channel, User, Event)
│   ├── store/
│   │   ├── store.go                 # in-memory state: сообщения, per-channel кэш
│   │   └── db.go                    # SQLite: messages, channels, ai_history
│   ├── ai/                          # планируется в AI-milestone
│   │   ├── agent.go
│   │   └── tools.go
│   └── tui/
│       ├── model.go                 # root Bubble Tea model
│       ├── msgs.go                  # tea.Msg типы
│       ├── keys.go                  # KeyMap для всех биндингов
│       ├── styles.go                # Lip Gloss стили
│       ├── command.go               # парсинг и регистрация команд
│       ├── help.go                  # popup помощи
│       ├── channels_view.go         # M2: боковая панель каналов
│       ├── messages_view.go         # M2: панель сообщений (заменяет feed)
│       ├── thread_popup.go          # M2: popup просмотра треда
│       ├── channel_filter_popup.go  # M2: popup поиска + сортировки + фильтрации (Ctrl+K)
│       ├── channel_info_popup.go    # M2: popup информации о канале (i)
│       └── views/
│           └── feed.go              # M1: общая лента (All Activity)
└── docs/
    ├── spec.md
    ├── backlog.md
    ├── m1/                          # M1 спецификация + decision logs (T1–T9)
    └── m2/                          # M2 спецификация + decision logs (T1–T8)
```

### Поток данных

```
WebSocket Events (MM API)
      │
      ▼
mattermost/websocket.go
      │  chan Event
      ▼
tui/model.go (handlePostedEvent)
      │  store.AddMessage()
      ├──────────────────────────────► store/db.go (SQLite)
      │
      │  если !activeChannel → обновить unread badge
      │  если activeChannel → добавить в messages panel
      ▼
MessagesView + ChannelsView (re-render)

Открытие канала (Enter):
      User selects channel → MsgChannelSelected
            │
            ▼
      client.GetChannelPosts(channelID, page, 100)  [tea.Cmd]
            │
            ▼
      MsgChannelHistory → MessagesView populated

Infinite scroll (скролл вверх до начала):
      client.GetChannelPosts(channelID, nextPage, 100)  [tea.Cmd]
            │
            ▼
      MsgChannelHistory{Prepend:true} → prepend to MessagesView

Открытие треда (Enter на сообщении):
      client.GetPostThread(postID)  [tea.Cmd]
            │
            ▼
      MsgThreadLoaded → ThreadPopup populated

Mark as read (при переключении на другой канал):
      client.MarkChannelRead(prevChannelID)  [fire-and-forget tea.Cmd]

(планируется в AI-milestone):
      └── (watched events) ──► ai/agent.go
                                    │
                                    ▼
                             Anthropic API (streaming + tool_use)
                                    │
                                    ▼
                             tools.go → mattermost/client.go
                                             + store/store.go
```

---

## 8. UX-принципы

- **Keyboard-first**: всё доступно с клавиатуры, мышь не требуется.
- **Non-blocking**: любая сетевая операция не блокирует TUI; индикатор в статус-баре.
- **Status bar**: всегда видно состояние соединения, текущий канал, total unread.
- **Confirm before act**: все деструктивные/публичные действия (отправка через AI, удаление) требуют `y/n`.
- **Graceful degradation**: если AI недоступен — остальное работает. При обрыве сети — показывает кэш из SQLite.

### Модель навигации

| Область | Клавиши |
|---|---|
| Переключение панелей | `Ctrl+L` (channels), `Ctrl+J` (messages), `Ctrl+B + ←/↑/↓` (prefix) |
| Список каналов | `↑` / `↓`, `PgUp`/`PgDn` навигация; `Enter` открыть; `i` инфо о канале |
| Список сообщений | `↑` / `↓`, `PgUp` / `PgDn`, `End` для последнего |
| Thread popup | `↑` / `↓` навигация; `r` ответить; `e` редактировать; `Esc` закрыть |
| Search/sort/filter popup | `Ctrl+K` открыть; `↑`/`↓` по результатам; `Enter` открыть/применить; `Esc` закрыть |
| Поле ввода | Стандартные стрелки и текстовые биндинги; `Cmd+Enter` отправить |
| AI-панель | `Ctrl+A` — открыть/скрыть (планируется) |
| Командная строка | `/` — открыть, `Esc` — закрыть |
| Очистить ввод | `Ctrl+C` (при непустом поле) |
| Отмена / закрыть | `Esc` |
| Выход из приложения | `/quit` |

---

## 9. Зависимости (go.mod)

```
github.com/charmbracelet/bubbletea
github.com/charmbracelet/lipgloss
github.com/charmbracelet/bubbles
github.com/coder/websocket           # WS-клиент, fork nhooyr.io/websocket
github.com/BurntSushi/toml
github.com/anthropics/anthropic-sdk-go
modernc.org/sqlite                   # pure Go SQLite, без CGO (добавляется в T7)
```
