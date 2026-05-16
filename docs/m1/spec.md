# M1 — Базовая лента + авторизация + отправка

> Спецификация реализации первого milestone.
> Версия: 1.1 | Дата: 2026-05-16

---

## Контекст и цель

M1 — минимальный рабочий клиент: авторизация по PAT, единая хронологическая лента всех каналов команды, отправка сообщений командой `/send`.

**Принятые решения:**
- Scope ленты: все каналы команды из `[server].team`
- Поиск канала при `/send #channel`: по slug-имени (`channel.Name`)
- `/send @username`: создаёт DM-канал и отправляет туда
- Инициализация данных: WS-only; REST-загрузка истории — M2
- Выход из приложения: команда `/quit`; `Ctrl+C` очищает поле ввода
- Модуль: `github.com/avalarin/mattermost-cli`, Go 1.23

---

## User Stories

### US-1: Авторизация через PAT
**Как** пользователь, работающий в терминале,  
**я хочу** указать URL сервера и Personal Access Token в конфиге  
**чтобы** подключиться к Mattermost без браузера.

**Acceptance criteria:**
- Приложение читает `~/.config/mattermost-cli/config.toml`
- Переменные `MATTERMOST_URL`, `MATTERMOST_TOKEN` перекрывают конфиг
- При невалидном токене — сообщение об ошибке в stderr и выход с кодом 1
- При отсутствии обязательных полей — понятная ошибка до запуска TUI

---

### US-2: Единая лента всех каналов команды
**Как** пользователь,  
**я хочу** видеть все входящие сообщения из всех каналов команды в одном хронологическом потоке  
**чтобы** не пропускать активность, не переключаясь между каналами.

**Acceptance criteria:**
- Лента показывает сообщения из всех каналов команды в порядке `create_at`
- Формат строки: `[HH:MM] #channel-name  username: текст сообщения`
- Треды отображаются как `[HH:MM] #channel  ↩ username: текст`
- Рядом с тредовым ответом — краткий контекст: `↩ В ответ на: <первые 40 символов parent>`
- Новые сообщения появляются снизу в реальном времени через WS

---

### US-3: Просмотр истории (скролл)
**Как** пользователь,  
**я хочу** прокручивать ленту сообщений клавишами  
**чтобы** просматривать историю без мыши.

**Acceptance criteria:**
- `↑`/`↓` — скролл на одну строку
- `PgUp`/`PgDn` — скролл на высоту viewport
- `End` — прыжок к последнему сообщению
- Позиция скролла сохраняется при приходе нового сообщения (не сбрасывается, если пользователь не у дна)
- Если пользователь находится у дна — новые сообщения автоматически скроллятся в вид

---

### US-4: Отправка сообщения
**Как** пользователь,  
**я хочу** отправить сообщение в канал или личным сообщением командой `/send`  
**чтобы** общаться не выходя из терминала.

**Acceptance criteria:**
- `/` открывает командную строку
- `/send #general Привет` отправляет сообщение в канал `general` (поиск по slug)
- `/send @username Привет` открывает DM-канал с пользователем и отправляет туда
- Если канал/пользователь не найден — ошибка в статус-баре, командная строка очищается
- После успешной отправки — командная строка очищается, статус-бар показывает подтверждение на 2 секунды
- `Esc` отменяет ввод команды

---

### US-5: Индикатор соединения и автореконнект
**Как** пользователь,  
**я хочу** видеть статус WS-соединения в шапке и знать что приложение само переподключается  
**чтобы** не беспокоиться о временных сетевых проблемах.

**Acceptance criteria:**
- Шапка всегда показывает статус: `[connected]` или `[reconnecting... Xs]`
- При обрыве — reconnect с exponential backoff: 1s, 2s, 4s, …, max 60s, jitter ±20%
- Счётчик в шапке показывает время до следующей попытки
- После успешного переподключения — статус `[connected]`

---

### US-6: Выход из приложения
**Как** пользователь,  
**я хочу** выйти из приложения командой `/quit`  
**чтобы** корректно завершить сессию.

**Acceptance criteria:**
- `/quit` корректно завершает TUI, закрывает WS и SQLite, возвращает управление терминалу
- `Ctrl+C` в режиме ввода — очищает поле ввода (не выходит)
- `Ctrl+C` при пустом поле ввода — показывает в статус-баре подсказку "To exit, use /quit"

---

## Структура пакетов (M1)

```
mattermost-cli/
├── cmd/
│   └── mattermost-cli/
│       └── main.go              # точка входа, флаги --debug, --config
├── internal/
│   ├── config/
│   │   └── config.go            # загрузка TOML + env override + валидация
│   ├── mattermost/
│   │   ├── client.go            # REST: GetTeam, GetCurrentUser, GetChannels, SendMessage, FindOrCreateDM
│   │   ├── websocket.go         # WS Events API, reconnect, exponential backoff
│   │   └── types.go             # Message, Channel, Team, User, Event (локальные типы)
│   ├── store/
│   │   ├── store.go             # in-memory состояние: messages, connStatus
│   │   └── db.go                # SQLite: инициализация схемы, read/write
│   └── tui/
│       ├── model.go             # root Bubble Tea Model, Init/Update/View
│       ├── views/
│       │   └── feed.go          # viewport с лентой сообщений
│       ├── keys.go              # KeyMap
│       └── styles.go            # Lip Gloss стили
├── .golangci.yml                # конфиг линтера
├── Makefile                     # build, test, lint
├── go.mod
└── go.sum
```

---

## Поток данных

```
WebSocket Events (MM API)
      │
      ▼
mattermost/websocket.go
      │  chan Event
      ├──────────────────────► store/db.go (SQLite persist)
      │
      ▼
store/store.go (in-memory messages list, conn status)
      │  tea.Msg
      ▼
tui/model.go → Update() → View()
      │
      ├── /send #channel ──► client.GetChannelByName → client.SendMessage
      └── /send @user    ──► client.FindOrCreateDM   → client.SendMessage
```

---

## Задачи

Каждая задача завершается состоянием, которое можно проверить руками — приложение запускается после каждой задачи и показывает новые возможности.

---

### ✅ T1: Project scaffolding + app skeleton

**Что делаем:**
- `go.mod` с `module github.com/avalarin/mattermost-cli`, `go 1.23` и всеми зависимостями
- Все пакеты создаются как stubs (компилируются, но ничего не делают)
- `.golangci.yml` с базовыми проверками (errcheck, govet, staticcheck, unused)
- `Makefile`: `make build`, `make test`, `make lint`
- `main.go`: парсит флаги `--debug`, `--config`; при `--debug` — slog handler на `debug.log`
- Запускает минимальный Bubble Tea TUI: экран с текстом "Config required. Run with --config path/to/config.toml" и выходит по `/quit`

**Критерии приемки:**
- `make build` завершается без ошибок
- `make test` запускается и проходит (пусть тестов пока мало)
- `make lint` не выдаёт ошибок

**Как проверить руками:**
1. `make build` → бинарь собирается
2. `./mattermost-cli` → TUI запускается, показывает сообщение об отсутствии конфига
3. Введи `/quit` → приложение корректно завершается, терминал в порядке
4. `Ctrl+C` при пустом поле → статус-бар показывает "To exit, use /quit", приложение не выходит

**Сценарии автотестов:**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestBuild` | `go build ./...` завершается без ошибок (exec-тест или просто `go vet ./...`) |
| 2 | `TestDefaultConfigPath` | При `--config` не задан — используется `~/.config/mattermost-cli/config.toml` |
| 3 | `TestCustomConfigPath` | `--config /tmp/test.toml` передаётся в `config.Load()` |
| 4 | `TestQuitCommandExits` | Модель получает команду `/quit` → возвращает `tea.Quit` |
| 5 | `TestCtrlCEmptyFieldShowsHint` | `Ctrl+C` при пустом input → статус-бар содержит "Use /quit" |

---

### ✅ T2: GitHub Actions CI

**Что делаем:**
- `.github/workflows/ci.yml` — запускается на каждый push и PR в `main`
- Шаги: `actions/checkout`, `actions/setup-go@v5` (Go 1.23), `make build`, `make lint`, `make test`
- Линтер устанавливается через `golangci/golangci-lint-action`
- Кэширование модулей (`actions/cache`) для ускорения прогонов

**Критерии приемки:**
- CI зелёный на первом коммите — сборка, линтер и тесты проходят
- Любой следующий PR не может быть смержен с красным CI

**Как проверить руками:**
1. Запушь коммит с T1 на GitHub → вкладка Actions показывает зелёный прогон
2. Сломай намеренно (например, добавь `var _ = unused`), запушь → CI красный
3. Исправь → CI снова зелёный

**Сценарии автотестов:**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | CI build step | `go build ./...` проходит в GitHub Actions |
| 2 | CI lint step | `golangci-lint run` проходит без ошибок |
| 3 | CI test step | `go test ./...` проходит без падений |

---

### ✅ T3: Config package

**Что делаем:**
- Структура `Config` с секциями `Server`, `AI`, `UI`
- `Load(path string) (*Config, error)`: читает TOML → применяет env override
- Env override: `MATTERMOST_URL`, `MATTERMOST_TOKEN`, `ANTHROPIC_API_KEY`
- Валидация: `Server.URL` и `Server.Token` обязательны; при отсутствии → `ErrMissingRequiredField`
- Defaults: `UI.DateFormat = "15:04"`, `UI.MessageLimit = 100`, `UI.Theme = "auto"`, `AI.Model = "claude-sonnet-4-6"`, `AI.Enabled = false`
- При успешной загрузке конфига — в статус-баре TUI показывается "Config loaded: server=url"

**Критерии приемки:**
- `Load()` возвращает ошибку с описанием при невалидном конфиге
- Env переменные перекрывают файл
- Приложение запускается с валидным конфигом и показывает URL сервера

**Как проверить руками:**
1. Без конфига: `./mattermost-cli` → сообщение "Config file not found" (не паника)
2. `cp config.example.toml config.dev.toml` → убери token → `./mattermost-cli --config config.dev.toml` → "Missing required field: server.token"
3. С полным `config.dev.toml`: → статус-бар показывает "Config loaded: server=https://..."
4. `MATTERMOST_URL=https://other.host ./mattermost-cli --config config.dev.toml` → URL в статус-баре изменился

**Сценарии автотестов:**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestLoadValidConfig` | Все поля из TOML попадают в структуру |
| 2 | `TestEnvOverridesConfig` | `MATTERMOST_URL` перекрывает значение из файла |
| 3 | `TestMissingURLReturnsError` | `Load()` возвращает ошибку если URL не задан |
| 4 | `TestMissingTokenReturnsError` | `Load()` возвращает ошибку если Token не задан |
| 5 | `TestDefaultValues` | При минимальном конфиге применяются значения по умолчанию |

---

### ✅ T4: Mattermost types + REST client

**Что делаем** (объединены бывшие T3+T4):
- Типы: `Team`, `Channel`, `User`, `Message`, `Event` и константы event-типов
- `Client`: `NewClient(url, token string)`, методы:
  - `GetTeamByName(name string) (*Team, error)`
  - `GetCurrentUser() (*User, error)`
  - `GetChannelsForTeam(teamID string) ([]Channel, error)`
  - `GetChannelByName(teamID, name string) (*Channel, error)` → `ErrChannelNotFound` при 404
  - `FindOrCreateDM(teamID, targetUserID string) (*Channel, error)`
  - `SendMessage(channelID, text, rootID string) (*Message, error)`
- На старте приложение аутентифицируется и показывает в шапке: `mattermost-cli [connecting] team: <team>  @<username>`

**Критерии приемки:**
- При неверном токене — приложение выводит ошибку и выходит с кодом 1 (до запуска TUI)
- При успешной авторизации — шапка показывает имя пользователя и команды

**Как проверить руками:**
1. С неверным токеном: `./mattermost-cli` → "Authentication failed: invalid token", код выхода 1
2. С верным конфигом: приложение запускается, в шапке видно `team: myteam  @john`
3. Нет сети: → понятная ошибка соединения (не паника)

**Сценарии автотестов (mock HTTP-сервер):**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestGetCurrentUser_OK` | Ответ 200 маппится в `User{ID, Username}` |
| 2 | `TestGetTeamByName_NotFound` | Ответ 404 возвращает понятную ошибку |
| 3 | `TestGetChannelByName_NotFound` | 404 возвращает `ErrChannelNotFound` |
| 4 | `TestSendMessage_AuthHeader` | Запрос содержит `Authorization: Bearer <token>` |
| 5 | `TestSendMessage_OK` | Успешная отправка возвращает `Message` с заполненным ID |
| 6 | `TestMessageIsReply` | `Message.RootID != ""` означает тред-ответ |

---

### T5: TUI feed + header + команды `/quit`

**Что делаем** (скелет TUI, виден с этой задачи):
- Root Bubble Tea model: `Init`, `Update`, `View`
- Шапка: `mattermost-cli  [connecting]  team: <name>  @<user>`
- Feed-панель: `bubbles/viewport`, при пустом feed — плейсхолдер "Waiting for messages..."
- Статус-бар (1 строка): временные сообщения об ошибках и подтверждениях
- Поле ввода: `bubbles/textinput`; `/` активирует режим команды; `Esc` отменяет
- `/quit` → graceful shutdown (закрывает WS/DB, возвращает управление)
- `Ctrl+C` при непустом поле → очищает поле
- `Ctrl+C` при пустом поле → статус-бар: "To exit, use /quit"
- Layout: `lipgloss.JoinVertical`: header(1) + feed(N) + statusbar(1) + input(1)
- Адаптируется к `tea.WindowSizeMsg`

**Критерии приемки:**
- TUI запускается, все зоны видны
- Можно набрать `/quit` и выйти
- `Ctrl+C` не выходит, показывает подсказку
- При ресайзе терминала — layout не ломается

**Как проверить руками:**
1. Запусти приложение → видны все 4 зоны (шапка, лента, статус-бар, поле ввода)
2. Нажми `/` → поле ввода активируется с префиксом `/`
3. Нажми `Esc` → поле очищается
4. Нажми `Ctrl+C` при пустом поле → статус-бар: "To exit, use /quit"
5. Введи `/quit` + `Enter` → приложение завершается, терминал не испорчен
6. Измени размер терминала → layout перестраивается корректно

**Сценарии автотестов:**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestSlashOpensCommandMode` | Нажатие `/` переводит model в `ModeCommand` |
| 2 | `TestEscCancelsCommand` | `Esc` в `ModeCommand` → `ModeNormal`, input очищен |
| 3 | `TestQuitCommandReturnsTeaQuit` | `/quit` + Enter → Update возвращает `tea.Quit` |
| 4 | `TestCtrlCClearsInput` | `Ctrl+C` при непустом поле → поле пустое, режим Normal |
| 5 | `TestCtrlCEmptyShowsHint` | `Ctrl+C` при пустом поле → статус-бар содержит "Use /quit" |
| 6 | `TestLayoutHeightFitsWindow` | Сумма высот компонентов = высота из `WindowSizeMsg` |

---

### T6: WebSocket client + сообщения в ленте

**Что делаем:**
- `WSClient`: подключение к `wss://<host>/api/v4/websocket`, auth-challenge фрейм
- Читает JSON-фреймы → `chan Event`; reconnect-loop с exponential backoff
- Backoff: `min(base * 2^attempt, 60s)`, jitter ×[0.8, 1.2]
- `chan ConnStatus` → шапка обновляется: `[connected]` / `[reconnecting... Xs]`
- WS-ивент `posted` → feed view получает новое сообщение (через `tea.Cmd`)
- Рендер сообщений: `[HH:MM] #channel  username: text`
- Тред-ответы: `[HH:MM] #channel  ↩ username: text  ↩ В ответ на: <snippet>`
- При `atBottom=true` → автоскролл при новом сообщении

**Критерии приемки:**
- Реальные сообщения из MM появляются в TUI-ленте в реальном времени
- При обрыве сети — шапка показывает countdown; после восстановления — `[connected]`
- Тредовые ответы отличаются визуально

**Как проверить руками:**
1. Запусти приложение → шапка показывает `[connected]`
2. Напиши сообщение в Mattermost в браузере → оно появляется в TUI-ленте
3. Напиши ответ в тред в браузере → он появляется с `↩` префиксом и сниппетом родителя
4. Отключи сеть → шапка меняется на `[reconnecting... 3s]` с обратным отсчётом
5. Верни сеть → шапка снова `[connected]`, новые сообщения приходят

**Сценарии автотестов:**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestWSConnect_SendsAuthChallenge` | После соединения клиент отправляет auth-фрейм |
| 2 | `TestWSReceivesPostedEvent` | JSON-фрейм `posted` → `Event{Type:"posted"}` в канале |
| 3 | `TestWSReconnectOnClose` | При закрытии сервером — клиент переподключается |
| 4 | `TestBackoffCapped` | После N попыток задержка не превышает 60 секунд |
| 5 | `TestBackoffJitter` | Две последовательные задержки различаются |
| 6 | `TestFeedRenderReply` | Тредовый ответ рендерится с `↩` и сниппетом родителя |

---

### T7: SQLite + in-memory store

**Что делаем** (объединены бывшие T6+T7):
- SQLite: `Open(path) (*DB, error)`, схема `channels` + `messages`, `InsertMessage`, `GetRecentMessages`, `GetMessageByID`
- In-memory `Store`: упорядоченный список сообщений (cap 1000), `AddMessage`, `GetParentSnippet`
- WS-ивенты сохраняются в SQLite; при следующем запуске приложение загружает их из БД
- `tea.Msg` типы: `MsgNewMessage`, `MsgConnStatus`, `MsgCommandResult`, `MsgClearStatus`

**Критерии приемки:**
- Сообщения сохраняются между перезапусками приложения
- Тред-ответы находят сниппет родителя даже если родитель пришёл до текущей сессии

**Как проверить руками:**
1. Получи несколько сообщений в приложении → закрой через `/quit`
2. Перезапусти приложение → сообщения из предыдущей сессии видны в ленте
3. Получи тредовый ответ на старое сообщение → сниппет родителя показывается корректно

**Сценарии автотестов:**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestOpenCreatesSchema` | После `Open()` таблицы `channels`, `messages` существуют |
| 2 | `TestInsertAndGetMessage` | Вставленное сообщение возвращается из `GetRecentMessages` |
| 3 | `TestInsertDuplicateIgnored` | Повторный `InsertMessage` с тем же ID не возвращает ошибку |
| 4 | `TestGetRecentMessagesOrdering` | Сообщения отсортированы по `create_at` (новые последними) |
| 5 | `TestGetParentSnippetFound` | Если parent в Store — возвращает первые 40 символов |
| 6 | `TestAddMessageCap` | При добавлении >1000 сообщений в Store — старые отбрасываются |

---

### T8: Команда `/send` + DM

**Что делаем:**
- `parseCommand(input string) (Command, error)`:
  - `/send #channel-name text` → `SendCommand{Target: "#channel-name", Text: "text"}`
  - `/send @username text` → `SendCommand{Target: "@username", Text: "text"}`
  - `/quit` → `QuitCommand{}`
  - Неизвестная команда → `ErrUnknownCommand`
  - Неверный формат → `ErrInvalidSyntax`
- `executeCommand` (неблокирующий `tea.Cmd`):
  - `#channel` → `GetChannelByName`; при `ErrChannelNotFound` → статус-бар с ошибкой
  - `@username` → `FindOrCreateDM`; при ошибке → статус-бар с ошибкой
  - При успехе → `SendMessage`, командная строка очищается, статус-бар: "Sent ✓" на 2 секунды

**Критерии приемки:**
- Отправка в канал и в DM работают
- Ошибочные команды не крашат приложение, показывают понятное сообщение

**Как проверить руками:**
1. Введи `/send #general Привет` → сообщение появляется в ленте через WS
2. Введи `/send @colleague Привет` → в ленте появляется DM
3. Введи `/send #nonexistent test` → статус-бар: "Channel not found: nonexistent"
4. Введи `/foo bar` → статус-бар: "Unknown command: foo"
5. Введи `/send` (без аргументов) → статус-бар: "Usage: /send #channel|@user text"

**Сценарии автотестов:**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestParseValidSendToChannel` | `/send #general Привет мир` → `SendCommand{Target:"#general", Text:"Привет мир"}` |
| 2 | `TestParseValidSendDM` | `/send @alice Привет` → `SendCommand{Target:"@alice", Text:"Привет"}` |
| 3 | `TestParseInvalidSendNoArgs` | `/send` без аргументов → `ErrInvalidSyntax` |
| 4 | `TestParseUnknownCommand` | `/foo bar` → `ErrUnknownCommand` |
| 5 | `TestExecuteSendChannelNotFound` | `GetChannelByName` → `ErrChannelNotFound` → статус-бар показывает ошибку |
| 6 | `TestExecuteSendDMSuccess` | Успешный `/send @user` → вызывает `FindOrCreateDM` затем `SendMessage` |

---

### T9: Клавиатурная навигация

**Что делаем:**
- `KeyMap` структура (совместима с `bubbles/help`)
- `↑`/`↓`: скролл ленты на 1 строку; в `ModeCommand` — не работают для feed
- `PgUp`/`PgDn`: скролл на высоту viewport
- `End`: скролл к последнему сообщению + `atBottom = true`
- Auto-scroll: при `atBottom=true` и приходе нового сообщения — viewport следует за лентой
- При скролле вверх — `atBottom = false`, auto-scroll отключается

**Критерии приемки:**
- Все клавиши работают в нормальном режиме
- В режиме ввода команды — навигационные клавиши не двигают ленту

**Как проверить руками:**
1. Подожди, пока в ленту придёт 30+ сообщений (или добавь через `/send`)
2. Нажми `↑` несколько раз → лента скроллится вверх, `atBottom=false`
3. Нажми `End` → скролл прыгает вниз, auto-scroll включается
4. Следующее сообщение → лента автоматически следует за ним
5. Нажми `/` → наберитай текст → `↑`/`↓` не двигают feed

**Сценарии автотестов:**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestUpArrowScrollsFeed` | `↑` в `ModeNormal` сдвигает viewport вверх |
| 2 | `TestEndJumpsToBottom` | `End` → `atBottom = true`, viewport в конце |
| 3 | `TestFeedAutoScrollAtBottom` | При `atBottom=true` и новом сообщении — viewport следует вниз |
| 4 | `TestFeedNoAutoScrollWhenScrolledUp` | При `atBottom=false` и новом сообщении — позиция не меняется |
| 5 | `TestNavKeysDisabledInCommandMode` | `↑`/`↓` в `ModeCommand` не меняют позицию feed |

---

## Порядок реализации

```
T1 (scaffold + skeleton)  →  app запускается
  └── T2 (GitHub CI)       →  CI зелёный на каждом PR
        └── T3 (config)    →  показывает URL из конфига
              └── T4 (REST + types)  →  показывает @username в шапке
                    └── T5 (TUI)    →  полный интерфейс виден, /quit работает
                          └── T6 (WS + feed)   →  живые сообщения в ленте
                                └── T7 (store) →  персистентность
                                      └── T8 (/send + DM)  →  отправка работает
                                            └── T9 (navigation)  →  полный UX
```

---

## Технические детали

### WebSocket reconnect backoff

```go
func backoffDuration(attempt int) time.Duration {
    base := time.Second
    cap  := 60 * time.Second
    d := base * (1 << min(attempt, 10))
    if d > cap {
        d = cap
    }
    jitter := 0.8 + rand.Float64()*0.4  // [0.8, 1.2]
    return time.Duration(float64(d) * jitter)
}
```

### Формат строки сообщения

```
[10:02] #backend  alice: PR готов, посмотрите
[10:03] #general  ↩ bob: привет!  ↩ В ответ на: привет всем, как де...
```

Ширина `[HH:MM]` = 7, `#channel-name` = до 20 символов, паддинг пробелами.

### tea.Msg типы (store → TUI)

```go
type MsgNewMessage    struct{ Msg mattermost.Message }
type MsgConnStatus    struct{ Status ConnStatus }
type MsgCommandResult struct{ Err error; Info string }
type MsgClearStatus   struct{}
```

### SQLite DSN

- Продакшен: `file:~/.config/mattermost-cli/db.sqlite?_journal=WAL`
- Тесты: `file::memory:?cache=shared`

---

## Критерии готовности M1

- [ ] `make build` — бинарь собирается без ошибок
- [ ] `make test` — все тесты зелёные
- [ ] `make lint` — линтер не выдаёт ошибок
- [ ] GitHub Actions CI зелёный на каждом коммите в main
- [ ] Приложение запускается с реальным Mattermost-сервером и показывает ленту
- [ ] `/send #channel text` — сообщение появляется в ленте через WS
- [ ] `/send @username text` — DM доставляется
- [ ] При обрыве сети — countdown в шапке, автоматическое переподключение
- [ ] `/quit` — корректный выход; `Ctrl+C` — только очищает поле
