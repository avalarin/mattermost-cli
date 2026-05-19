# M2 — Каналы, переключение, непрочитанные, треды

> Спецификация реализации второго milestone.
> Версия: 1.1 | Дата: 2026-05-17

---

## Контекст и цель

M2 превращает единую ленту M1 в полноценный клиент с боковой панелью каналов, фильтрацией по каналу, просмотром истории через REST API, счётчиками непрочитанных и панелью треда.

**Принятые решения:**
- «All Activity» остаётся как первый пункт в списке каналов (поведение M1 по умолчанию)
- История канала грузится и при открытии канала, и при скролле вверх (infinite scroll)
- `/reload` — ручная подгрузка дополнительной истории (только когда выбран конкретный канал)
- DM-каналы отображаются в одном списке с публичными/приватными каналами
- `/watch` — откладывается до AI-milestone
- Навигация по панелям — только горячие клавиши (Tab/Shift+Tab убраны):
  - `Ctrl+B` — prefix-клавиша в стиле tmux: нажимается первой, затем стрелка определяет цель:
    - `Ctrl+B → ↑` или `Ctrl+B → →` → ModeMessages
    - `Ctrl+B → ↓` → ModeInput
    - `Ctrl+B → ←` → ModeChannels
    - Любая нестрелочная клавиша — отмена
    - Окно ожидания второй клавиши: 1 с, после — автоотмена
  - `Ctrl+J` → messages (сохраняется из M1)
  - `Ctrl+L` → channels (новое)
  - `Ctrl+K` → search popup (новое)
  - `Esc` → всегда возвращает в input
- T4 «Панель треда» — popup поверх основного контента (перекрывает channels и messages), input остаётся снизу
- T5 «Непрочитанные» — `MarkChannelRead` вызывается при **уходе из канала** (не при скролле до дна)
- T6 «Поиск» — ищет и каналы, и пользователей; выбор пользователя открывает DM

---

## User Stories

### US-1: Список каналов и переключение

**Как** пользователь,  
**я хочу** видеть список всех каналов в боковой панели и переключаться между ними  
**чтобы** читать и писать в нужном канале, не выходя из терминала.

**Acceptance criteria:**
- Левая панель показывает «All Activity» (закреплён первым) и все каналы, доступные пользователю (включая DM), отсортированные по алфавиту
- Тип канала обозначается префиксом: `#` канал (публичный и приватный), `@` DM
- `↑`/`↓`, `PgUp`/`PgDn` — навигация по списку, **только выбор** (не открытие); выбранный элемент подсвечен светло-серым фоном
- Список поддерживает скролл, если каналов больше, чем высота панели
- `Enter` — **открытие** канала: его сообщения загружаются в панель messages; открытый канал подсвечивается белым фоном (текст чёрный)
- `Ctrl+L` — фокус в панель channels из любого режима
- `Esc` из channels → input
- Архивированные/недоступные каналы отображаются с маркером `[x]`; при попытке открыть — ошибка в статус-баре

---

### US-2: История канала (REST) + infinite scroll + /reload

**Как** пользователь,  
**я хочу** видеть историю сообщений канала, в том числе до текущей сессии,  
**чтобы** не пропускать контекст.

**Acceptance criteria:**
- При открытии канала — сразу подгружаются последние N (=100) сообщений из REST API
- При прокрутке вверх до края — автоматически подгружается более старая история
- `/reload` в контексте конкретного канала — вручную подгружает ещё N сообщений вверх
- `/reload` без выбранного канала (или в «All Activity») — показывает ошибку в статус-баре
- Индикатор загрузки отображается в шапке по центру экрана при ожидании REST-ответа

---

### US-3: Только root-сообщения + бейдж ответов

**Как** пользователь,  
**я хочу** видеть в панели канала только исходные сообщения (без ответов в тред),  
и знать сколько ответов у каждого  
**чтобы** понимать, где есть активное обсуждение.

**Acceptance criteria:**
- Панель канала показывает только root-сообщения (`root_id == ""`); управляется настройкой `[ui] channel_messages = "root_only" | "all"` (по умолчанию `"root_only"`)
- Рядом с root-сообщением, у которого есть ответы, отображается бейдж `⤵︎ N`
- При получении WS-события нового ответа — счётчик у родительского сообщения обновляется
- «All Activity» по-прежнему показывает все сообщения (включая ответы) как в M1; ответы отображаются с бейджем `⤴︎` (символ указывает на родителя)

---

### US-4: Панель треда

**Как** пользователь,  
**я хочу** открыть тред сообщения и прочитать все ответы в отдельной панели  
**чтобы** следить за обсуждением не теряя контекст основной ленты.

**Acceptance criteria:**
- Enter на выбранном сообщении в панели сообщений → открывает панель треда справа; фокус переходит в тред
- Панель треда показывает root-сообщение и все его ответы (из REST)
- Панель треда **не перекрывает input**: пользователь работает с сообщениями треда так же, как в messages (курсор ↑/↓, те же горячие клавиши действий)
- Горячие клавиши в панели треда на выбранном сообщении:
  - `r` — вставляет `/reply` в input и переходит в input (для ответа в тред)
  - `e` — (только если автор = текущий пользователь) вставляет `/edit <текст сообщения>` в input и переходит в input
  - `d` — (только если автор = текущий пользователь) открывает popup подтверждения удаления
- Esc из панели треда — закрывает тред, фокус возвращается в messages
- `Ctrl+B` из треда — фокус в input (тред остаётся открытым)
- Команда `/reply` в M2 вставляет rootID текущего треда как контекст для следующей отправки через `/send`

---

### US-5: Счётчики непрочитанных + пометка прочитанным

**Как** пользователь,  
**я хочу** видеть сколько непрочитанных сообщений в каждом канале,  
и чтобы канал помечался прочитанным когда я дочитал до конца  
**чтобы** знать куда идти первым делом.

**Acceptance criteria:**
- При старте у каждого канала показывается количество непрочитанных: `#general (3)`
- При получении нового WS-сообщения в не-активном канале — счётчик увеличивается на 1
- Когда пользователь скроллит до последнего сообщения активного канала — канал помечается прочитанным через REST (`MarkChannelRead`) и счётчик сбрасывается
- «All Activity» не отображает счётчик

---

### US-6: Поиск каналов

**Как** пользователь,  
**я хочу** быстро найти канал по имени  
**чтобы** не скроллить длинный список.

**Acceptance criteria:**
- `Ctrl+K` (из любого режима) открывает popup поиска каналов по центру экрана
- Ввод текста фильтрует список в реальном времени; навигация `↑`/`↓` по результатам
- `Enter` — открывает выбранный канал; канал становится «выбранным» в channels panel
- `Ctrl+C` или `Esc` — закрывает popup без открытия канала
- В нижней части popup показаны горячие клавиши: `↑↓ navigate · Enter open · Esc/Ctrl+C close`

---

### US-7: Архивированные и недоступные каналы

**Как** пользователь,  
**я хочу** видеть недоступные каналы в списке, но получать понятную ошибку при попытке открыть их  
**чтобы** понимать, что канал существует, но недоступен.

**Acceptance criteria:**
- Архивированные и недоступные каналы показываются в списке с маркером `[x]` перед именем
- При попытке открыть такой канал — сообщение об ошибке в статус-баре: «Channel #name is archived»
- Панель messages не обновляется при попытке открыть недоступный канал

---

### US-8: Информация о канале

**Как** пользователь,  
**я хочу** быстро посмотреть информацию о канале прямо из боковой панели  
**чтобы** понять тему канала и кто в нём состоит, не открывая браузер.

**Acceptance criteria:**
- `i` при фокусе в channels panel (ModeChannels) → открывает popup с информацией о выбранном канале
- Popup показывает: название, описание, список участников
- `Esc` или `Enter` закрывают popup и возвращают фокус в channels
- Если описание пустое — показывается прочерк; участники загружаются из REST

---

## Архитектура (изменения относительно M1)

### Новые и изменённые файлы

```
internal/
├── mattermost/
│   ├── types.go         # Channel: +DisplayName, +Type, +DeleteAt; Message: +ReplyCount; ChannelUnread
│   └── client.go        # +GetChannelPosts, +GetPostThread, +MarkChannelRead, +GetChannelUnreads, +GetChannelMembers
├── config/
│   └── config.go        # UI: +ChannelsWidth(22), +ThreadWidth(40), +ChannelMessages("root_only")
├── store/
│   ├── db.go            # channels: +display_name, +type; +GetMessagesByChannel, +GetEarliestInChannel
│   └── store.go         # per-channel message map; +GetChannelMessages, +LoadChannelHistory
└── tui/
    ├── model.go         # +ModeChannels, +ModeThread, +ModeSearch, +ModeInfo; activeChannelID; per-panel layout
    ├── keys.go          # +FocusChannels(ctrl+l), +SearchChannels(ctrl+k); remove Tab/ShiftTab
    ├── msgs.go          # +MsgChannelSelected, +MsgChannelHistory, +MsgThreadLoaded, +MsgUnreadsLoaded
    └── views/
        ├── channels.go       # ChannelsView — список каналов с бейджами        [новый]
        ├── messages.go       # MessagesView — отфильтрованная лента канала      [новый, заменяет feed.go]
        ├── thread.go         # ThreadView — панель треда                        [новый]
        ├── channel_search.go # ChannelSearchPopup — popup поиска                [новый]
        └── channel_info.go   # ChannelInfoPopup — popup информации о канале     [новый]
```

### Режимы фокуса (Mode)

| Режим | Описание |
|---|---|
| `ModeInput` | textarea в фокусе (поведение M1) |
| `ModeChannels` | навигация по списку каналов |
| `ModeMessages` | курсор по сообщениям (поведение M1) |
| `ModeThread` | навигация внутри панели треда |

### Поток данных (M2)

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
      User selects channel
            │
            ▼
      MsgChannelSelected
            │
            ▼
      client.GetChannelPosts(channelID, 0, 100)  [tea.Cmd]
            │
            ▼
      MsgChannelHistory → MessagesView populated

Бесконечный скролл:
      User scrolls to top of MessagesView
            │
            ▼
      client.GetChannelPosts(channelID, page, 100)  [tea.Cmd]
            │
            ▼
      MsgChannelHistory → prepend to MessagesView

Открытие треда (Enter на сообщении):
      client.GetPostThread(postID)  [tea.Cmd]
            │
            ▼
      MsgThreadLoaded → ThreadView populated

Mark as read (при скролле в дно):
      client.MarkChannelRead(channelID)  [fire-and-forget tea.Cmd]
```

---

## Задачи

Каждая задача завершается состоянием, которое можно проверить руками.

---

### ✅ T1: Многопанельный layout + боковая панель каналов

**Что делаем:**

*Layout:*
- Разделяем экран на 2 колонки: `[channels(ui.channels_width, default 22) | messages(остальное)]`
- `View()` использует `strings` / `lipgloss` для side-by-side рендера
- Адаптируется к `tea.WindowSizeMsg`
- Резервируем место для третьей колонки треда (появится в T4)

*`views/channels.go` — `ChannelsView`:*
- Список каналов: «All Activity» (закреплён вверху) + все доступные пользователю каналы + DM, отсортированные по алфавиту
- Два состояния элемента: **selected** (курсор навигации, светло-серый фон) и **open** (активно открытый, белый фон, чёрный текст)
- `↑`/`↓`, `PgUp`/`PgDn` — навигация (только выбор, не открытие); список скроллируется
- Показывает тип канала: `#` канал (публичный и приватный), `@` DM
- Архивированные каналы — с маркером `[x]`
- Заголовок «Channels»

*`views/messages.go` — `MessagesView`:*
- Переносит функциональность feed из `model.go`: viewport, feedItems, renderMessageLine
- Принимает channel ID фильтр; при `channelID=""` (All Activity) — показывает всё
- Заголовок панели: «All Activity» или «#channel-name»

*Новые режимы и клавиши:*
- `ModeChannels`: ↑/↓/PgUp/PgDn навигация по каналам; Enter → `MsgChannelSelected`
- Esc из Channels → Input
- `Ctrl+L` → ModeChannels (новое)
- `Ctrl+J` → ModeMessages (M1, сохраняется без изменений)
- `Ctrl+B` → ModeInput (M1, сохраняется)
- Tab/Shift+Tab убраны

*`model.go`:*
- `activeChannelID string` — пустая строка = All Activity
- `channelsView ChannelsView`
- `messagesView MessagesView`
- Убираем прямой `viewport` и `feedItems` из Model, переносим в MessagesView

**Критерии приемки:**
- Старое поведение M1 полностью сохранено в «All Activity»
- Боковая панель видна: каналы отсортированы по алфавиту, DM в общем списке
- ↑/↓ в ModeChannels только выбирают (светло-серый), не открывают
- Enter открывает канал (белый фон) и обновляет messages
- Ctrl+L переходит в ModeChannels, Esc возвращает в input

**Как проверить руками:**
1. Запусти → видны 2 панели: список каналов слева (отсортировано), лента справа
2. Ctrl+L → фокус в каналах (первый элемент подсвечен светло-серым)
3. ↑/↓ → движение по списку, messages не меняется
4. Enter → канал открывается (белый фон), в messages появляются его сообщения
5. Ctrl+B → фокус в input
6. Esc из channels → input
7. Ресайз терминала → layout перестраивается корректно

**Сценарии автотестов:**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestCtrlLActivatesChannelsMode` | Ctrl+L → ModeChannels |
| 2 | `TestEscFromChannelsGoesToInput` | Esc в ModeChannels → ModeInput |
| 3 | `TestChannelsViewArrowSelectsOnly` | ↑/↓ меняют selected, не openChannelID |
| 4 | `TestChannelsViewEnterOpensChannel` | Enter → openChannelID обновляется, MsgChannelSelected отправляется |
| 5 | `TestLayoutWidthSplit` | Ширина channels + messages = общая ширина |
| 6 | `TestChannelsViewOpenHighlight` | Открытый канал рендерится белым фоном, выбранный — серым |

---

### ✅ T2: Переключение канала + REST-история + infinite scroll + /reload

**Что делаем:**

*`mattermost/client.go`:*
- `GetChannelPosts(channelID string, page, perPage int) ([]Message, error)`
  — GET `/channels/{channelId}/posts?page=N&per_page=100&sort=create_at`
- Возвращает только root-сообщения + replies сгруппированы в `[]Message` (сортировка по create_at)

*`mattermost/types.go`:*
- `Channel.DisplayName string`, `Channel.Type string` (O/P/D)
- `Message.ReplyCount int`

*`store/store.go`:*
- `channelMessages map[string][]Message` — per-channel кэш (cap 500 на канал)
- `AddChannelMessages(channelID string, msgs []Message, prepend bool)` — добавляет сверху или снизу
- `GetChannelMessages(channelID string) []Message`
- `loadedPages map[string]int` — сколько страниц загружено для каждого канала (для infinite scroll)

*`tui/msgs.go`:*
- `MsgChannelSelected{ ChannelID string }` — команда открыть канал
- `MsgChannelHistory{ ChannelID string; Messages []mattermost.Message; Prepend bool; Err error }` — результат REST
- `MsgChannelHistoryLoading{ ChannelID string }` — для индикатора

*`model.go`:*
- Enter на канале → `MsgChannelSelected` → запускает `loadChannelHistoryCmd(channelID, page=0)`
- `loadChannelHistoryCmd` — `tea.Cmd`, возвращает `MsgChannelHistory`
- MessagesView показывает индикатор загрузки, пока история не готова
- Скролл до начала MessagesView (ScrollPercent == 0) → автоматически запускает следующую страницу
- `activePage[channelID]` — счётчик страниц
- При выборе «All Activity» — фильтр снимается, история не перезагружается

*Команда `/reload`:*
- Регистрируем в Registry
- Если `activeChannelID == ""` → `MsgCommandResult{Err: "not in a channel"}`
- Иначе → `loadChannelHistoryCmd(activeChannelID, nextPage, prepend=true)`

**Критерии приемки:**
- Enter на канале → сообщения канала появляются в правой панели
- «All Activity» показывает общую ленту без изменений
- При скролле до начала → подгружается следующая страница истории
- `/reload` в канале → подгружает ещё сообщения сверху
- `/reload` в «All Activity» → ошибка в статус-баре

**Как проверить руками:**
1. Выбери конкретный канал → видны только его сообщения + история
2. Прокрути вверх до начала → подгружаются более старые сообщения
3. Введи `/reload` → ещё страница истории появляется сверху
4. Вернись в «All Activity» → видна общая лента
5. `/reload` в «All Activity» → "Not in a channel: use /reload only inside a specific channel"

**Сценарии автотестов (mock HTTP):**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestGetChannelPosts_OK` | Ответ 200 маппится в `[]Message` с `ReplyCount` |
| 2 | `TestGetChannelPosts_Pagination` | Параметры `page` и `per_page` передаются в запрос |
| 3 | `TestChannelSelectLoadsHistory` | MsgChannelSelected → MsgChannelHistory через tea.Cmd |
| 4 | `TestReloadCommandActiveChannel` | `/reload` в активном канале → loadChannelHistoryCmd |
| 5 | `TestReloadCommandNoChannel` | `/reload` без активного канала → MsgCommandResult{Err} |
| 6 | `TestStoreAddChannelMessages` | Сообщения хранятся per-channel и возвращаются в порядке create_at |

---

### ✅ T3: Только root-сообщения + бейдж ответов

**Что делаем:**

*`config/config.go`:*
- Добавляем `UI.ChannelMessages string` — `"root_only"` (по умолчанию) или `"all"`
- Управляет отображением ответов в панели канала; «All Activity» всегда показывает всё

*`views/messages.go`:*
- В фильтрованном виде (конкретный канал) фильтрует по `msg.RootID == ""` когда `ChannelMessages == "root_only"`
- В «All Activity» — все сообщения (поведение M1)
- `renderMessageLine` для root-сообщений с `ReplyCount > 0` добавляет бейдж `⤵︎ N`
- `renderMessageLine` для ответов в «All Activity» добавляет бейдж `⤴︎` (без числа)

*Отслеживание `ReplyCount`:*
- REST `GetChannelPosts` уже возвращает `reply_count` в каждом посте
- WS `posted` event: если `post.RootID != ""` → ищем в store родительский пост и увеличиваем его `ReplyCount` на 1
- `store.IncrementReplyCount(rootID string)` — обновляет в памяти и SQLite

*`store/db.go`:*
- `messages` таблица: добавляем колонку `reply_count INTEGER NOT NULL DEFAULT 0`
- `IncrementReplyCount(id string) error` — `UPDATE messages SET reply_count = reply_count + 1 WHERE id = ?`

**Критерии приемки:**
- В конкретном канале (при `channel_messages = "root_only"`) видны только root-сообщения
- Рядом с сообщением, имеющим ответы — `⤵︎ N`
- В «All Activity» ответы видны с бейджем `⤴︎`
- Счётчик увеличивается при получении нового ответа через WS
- При `channel_messages = "all"` ответы отображаются в канале тоже

**Как проверить руками:**
1. Зайди в канал с тредами → видны только root-сообщения
2. Рядом с сообщением с ответами — `⤵︎ 3` (или другое число)
3. Напиши ответ в тред через браузер → счётчик у родителя увеличился
4. «All Activity» — ответы всё ещё видны с `⤴︎`
5. Смени `channel_messages = "all"` в конфиге, перезапусти → ответы видны в канале тоже

**Сценарии автотестов:**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestMessagesViewFilterRoot` | `root_only`: items с RootID != "" не попадают в рендер |
| 2 | `TestMessagesViewShowAll` | `all`: items с RootID != "" тоже показываются |
| 3 | `TestAllActivityShowsReplies` | All Activity всегда показывает ответы |
| 4 | `TestReplyCountBadgeRoot` | renderMessageLine с ReplyCount=3 → строка содержит "⤵︎ 3" |
| 5 | `TestReplyBadgeInAllActivity` | renderMessageLine для ответа в All Activity → строка содержит "⤴︎" |
| 6 | `TestIncrementReplyCount` | WS posted reply → IncrementReplyCount вызывается для родителя |
| 7 | `TestIncrementReplyCountDB` | После IncrementReplyCount(id) — `GetMessageByID(id).ReplyCount` == 1 |

---

### ✅ T4: Панель треда (popup)

**Что делаем:**

*`mattermost/client.go`:*
- `GetPostThread(rootID string) ([]Message, error)` — GET `/posts/{postId}/thread`
- Возвращает root + все replies, отсортированные по `create_at`

*`tui/views/thread_popup.go` — `ThreadPopup`:*
- Overlay поверх channels и messages (не side panel)
- Занимает центральную область экрана (ширина: `total - 2` или `min(80, total-4)`, высота: `total_h - 3` — оставляем место input снизу)
- `viewport.Model` внутри
- Показывает root-сообщение + все replies (те же renderMessageLine, что и в основной ленте)
- Заголовок: «Thread — #channel-name»
- ↑/↓ / PgUp/PgDn — прокрутка внутри popup

*`tui/msgs.go`:*
- `MsgThreadLoaded{ RootID string; Messages []mattermost.Message; Err error }`

*Команды Registry (новые):*
- `/reply` — устанавливает контекст ответа в тред: следующий `/send` отправляет с `rootID` открытого треда; после отправки контекст сбрасывается

*`model.go`:*
- `threadPopup *ThreadPopup` — `nil` когда закрыт
- `openThreadID string` — ID root-сообщения открытого треда
- Enter на выбранном сообщении в ModeMessages → `loadThreadCmd(postID)` → MsgThreadLoaded → открывает popup, фокус в ModeThread
- Esc из ModeThread → закрываем threadPopup, переходим в ModeMessages
- `Ctrl+B` из ModeThread → переходим в ModeInput (popup остаётся открытым)
- В ModeThread горячие клавиши на выбранном сообщении:
  - `r` → вставляет `/reply` в input + ModeInput (popup остаётся)
  - `e` → (если `msg.UserID == currentUserID`) вставляет `/edit <текст>` в input + ModeInput
  - `d` → (если `msg.UserID == currentUserID`) заглушка — ошибка в статус-баре «Not implemented yet» (полная реализация в M3)
- Layout: при `threadPopup != nil` → рендерим popup поверх через `lipgloss.Place`; layout каналов и messages не меняется

**Критерии приемки:**
- Enter на сообщении → popup перекрывает channels и messages; фокус в треде
- Input снизу остаётся доступным
- Esc → popup закрывается, фокус в messages
- Содержимое треда прокручивается
- `r` вставляет `/reply` в input и переключает в input (popup остаётся)
- `e` на своём сообщении вставляет `/edit <текст>` в input
- Ctrl+B из треда → input (popup не закрывается)

**Как проверить руками:**
1. Выбери сообщение с ответами (`⤵︎ N`), нажми Enter → popup перекрывает экран, кроме input
2. В popup видны root + все ответы; курсор на последнем
3. ↑/↓ — навигация по сообщениям треда
4. Нажми `r` → input заполняется `/reply`, фокус в input, popup остаётся
5. Esc → popup закрывается

**Сценарии автотестов:**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestGetPostThread_OK` | Ответ API маппится в `[]Message` root+replies |
| 2 | `TestEnterOpensThread` | Enter на сообщении → threadPopup != nil, ModeThread |
| 3 | `TestEscClosesThread` | Esc из ModeThread → threadPopup == nil, ModeMessages |
| 4 | `TestCtrlBFromThreadGoesToInput` | Ctrl+B из ModeThread → ModeInput, threadPopup != nil |
| 5 | `TestThreadRKeyInsertsReply` | `r` в ModeThread → input содержит "/reply", ModeInput |

---

### ✅ T5: Счётчики непрочитанных + пометка прочитанным

**Что делаем:**

*`mattermost/types.go`:*
- `ChannelUnread{ ChannelID string; MsgCount int; MentionCount int }`

*`mattermost/client.go`:*
- `GetChannelUnreads(channelID string) (*ChannelUnread, error)` — GET `/users/me/channels/{channelId}/unread`
- `MarkChannelRead(channelID string) error` — POST `/channels/{channelId}/members/me/view`

*`model.go`:*
- `unreadCounts map[string]int` — channelID → количество непрочитанных
- При старте: `loadUnreadsCmd()` — параллельно запрашивает unreads для всех каналов
  - результат: `MsgUnreadsLoaded{ Counts map[string]int }`
- WS `posted` в не-активном канале → `unreadCounts[channelID]++`
- При переключении **из** канала (MsgChannelSelected с новым channelID) → `markReadCmd(prevChannelID)` (fire-and-forget)
  - результат: `MsgChannelRead{ ChannelID string }` → `unreadCounts[channelID] = 0`
- «All Activity» не вызывает MarkChannelRead
- `ChannelsView` получает `unreadCounts` при каждом рендере

*`views/channels.go`:*
- Рендер строки: `#general (3)` если count > 0, иначе `#general`
- «All Activity» — без счётчика

*`tui/msgs.go`:*
- `MsgUnreadsLoaded{ Counts map[string]int; Err error }`
- `MsgChannelRead{ ChannelID string }`

**Критерии приемки:**
- При старте у каналов с непрочитанными — счётчик в скобках
- При получении нового сообщения в не-активном канале — счётчик увеличивается
- При переключении на другой канал — предыдущий помечается прочитанным и счётчик сбрасывается
- «All Activity» счётчик не показывает

**Как проверить руками:**
1. Запусти → у канала с непрочитанными виден счётчик `(N)`
2. Напиши сообщение через браузер в неактивном канале → счётчик увеличивается
3. Зайди в тот канал, затем переключись в другой → счётчик исчезает в предыдущем

**Сценарии автотестов:**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestGetChannelUnreads_OK` | GET /unread маппится в `ChannelUnread` |
| 2 | `TestMarkChannelRead_CallsAPI` | markReadCmd отправляет POST на /view |
| 3 | `TestUnreadIncrementsOnWSPost` | WS posted в неактивном канале → unreadCounts[channelID]++ |
| 4 | `TestUnreadClearsOnChannelSwitch` | Переключение из канала → markReadCmd запускается для предыдущего |
| 5 | `TestChannelsViewRendersUnreadBadge` | ChannelsView рендерит `#general (3)` при count>0 |

---

### T6: Поиск каналов и пользователей (popup)

**Что делаем:**

*`mattermost/client.go`:*
- `SearchChannels(teamID, query string) ([]Channel, error)` — GET `/teams/{teamId}/channels/search?term=query`
- `SearchUsers(query string) ([]User, error)` — GET `/users/search` с `{ term: query }`

*Правим уже существующий popup для сортировки и фильтрации:*
- Структура:
  ```
  ┌─ Search ──────────────────────────────────────┐
  │ > query_                                      │
  ├───────────────────────────────────────────────┤
  │ ● #general                                    │
  │   #backend                                    │
  │   @alice                                      │
  ├───────────────────────────────────────────────┤
  │ SORT                                          │
  │ ○ Alphabetical                                │
  │ ● Last message                                │
  │ FILTERS                                       │
  │ ☐ Unread only                                 │
  ├───────────────────────────────────────────────┤
  │ ↑↓ navigate · Space toggle · Enter open/apply │
  │ Esc close                                     │
  └───────────────────────────────────────────────┘
  ```
- Поле ввода вверху; список результатов (каналы `#`, потом пользователи `@`); хоткеи внизу
- «All Activity» всегда первым пунктом
- Поиск: при вводе от 2+ символов — REST-запрос (`SearchChannels` + `SearchUsers`); при < 2 символов — показываем все локальные каналы
- Результаты дедуплицируются с уже открытыми каналами

*`model.go`:*
- `Ctrl+K` (из любого режима) → открывает `searchPopup != nil`, устанавливает ModeSearch
- Символы → фильтруют/ищут; `↑`/`↓` → навигация по результатам
- `Enter` на канале → `MsgChannelSelected`, канал становится открытым в channels panel; popup закрывается
- `Enter` на пользователе → `FindOrCreateDM`, потом `MsgChannelSelected`; popup закрывается
- `Esc` или `Ctrl+C` → закрывает popup без изменений
- `Backspace` → удаляет последний символ

*`keys.go`:*
- Добавляем `SearchChannels key.Binding` — `Ctrl+K`

*`tui/msgs.go`:*
- `MsgSearchResults{ Channels []mattermost.Channel; Users []mattermost.User; Err error }`

**Критерии приемки:**
- `Ctrl+K` из любого режима открывает popup по центру
- При < 2 символов — показывает все каналы
- При 2+ символов — REST-поиск, показывает каналы и пользователей
- «All Activity» всегда первым
- Enter на канале — открывает канал, channels panel подсвечивает его открытым
- Enter на пользователе — открывает DM с ним
- Esc и Ctrl+C закрывают popup
- Внизу popup видны хоткеи

**Как проверить руками:**
1. Нажми `Ctrl+K` → popup, показаны все каналы
2. Введи `ge` → REST-поиск, каналы `#general...` + пользователи `@george...`
3. Enter на канале → popup закрывается, messages показывает канал
4. Снова Ctrl+K, Enter на пользователе → открывается DM
5. Esc → popup закрывается

**Сценарии автотестов:**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestCtrlKOpensSearchPopup` | Ctrl+K → searchPopup != nil, ModeSearch |
| 2 | `TestSearchPopupShortQueryShowsLocal` | < 2 символов → только локальные каналы |
| 3 | `TestSearchPopupAlwaysShowsAllActivity` | «All Activity» всегда первым |
| 4 | `TestSearchPopupEscCloses` | Esc → searchPopup == nil |
| 5 | `TestSearchPopupEnterOpensChannel` | Enter на канале → MsgChannelSelected, searchPopup == nil |
| 6 | `TestSearchPopupEnterOpensDM` | Enter на пользователе → FindOrCreateDM → MsgChannelSelected |

---

### T7: Архивированные и недоступные каналы

**Что делаем:**

*`mattermost/types.go`:*
- `Channel.DeleteAt int64` — ненулевое означает архивированный канал

*`views/channels.go`:*
- Архивированные каналы (`DeleteAt > 0`) отображаются с маркером `[x]` перед именем
- Визуально приглушены (серый цвет)

*`model.go`:*
- При Enter на архивированном канале → `MsgCommandResult{Err: "Channel #name is archived"}`, messages не обновляется

**Критерии приемки:**
- Архивированные каналы видны в списке с `[x]`
- Попытка открыть → ошибка в статус-баре, messages не меняется

**Как проверить руками:**
1. Если в MM есть архивированный канал → он виден с `[x]`
2. Enter на нём → статус-бар: «Channel #name is archived»

**Сценарии автотестов:**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestArchivedChannelRendersWithMarker` | Channel.DeleteAt > 0 → рендер содержит "[x]" |
| 2 | `TestArchivedChannelEnterShowsError` | Enter на архивированном → MsgCommandResult{Err} |

---

### T8: Информация о канале (popup)

**Что делаем:**

*`mattermost/client.go`:*
- `GetChannelMembers(channelID string) ([]User, error)` — GET `/channels/{channelId}/members` + resolve usernames

*`views/channel_info.go` — `ChannelInfoPopup`:*
- Popup по центру экрана, структура:
  ```
  ┌─ #general ─────────────────────────────────┐
  │ Description                                 │
  │ General discussion channel                  │
  ├─────────────────────────────────────────────┤
  │ Members (12)                                │
  │ @alice  @bob  @charlie  @dave ...           │
  ├─────────────────────────────────────────────┤
  │ Enter/Esc — close                           │
  └─────────────────────────────────────────────┘
  ```
- Описание: строка `channel.Purpose`; если пусто — «No description»
- Участники: загружаются REST-запросом при открытии popup; до загрузки — «Loading...»

*`tui/msgs.go`:*
- `MsgChannelMembersLoaded{ ChannelID string; Members []mattermost.User; Err error }`

*`model.go`:*
- `i` в ModeChannels → `infoView = &ChannelInfoPopup{channel: selectedChannel}`, запускает `loadMembersCmd(channelID)`
- `Enter` или `Esc` → `infoView = nil`, фокус обратно в ModeChannels

**Критерии приемки:**
- `i` на канале → popup с названием, описанием, участниками
- Участники загружаются из REST и отображаются списком
- Enter или Esc закрывают popup

**Как проверить руками:**
1. Ctrl+L → channels, выбери канал, нажми `i` → popup открывается
2. В popup видны название, описание, список участников (@username)
3. Enter или Esc → popup закрывается, фокус в channels

**Сценарии автотестов:**

| # | Название | Что проверяем |
|---|----------|---------------|
| 1 | `TestGetChannelMembers_OK` | GET /members маппится в `[]User` |
| 2 | `TestInfoKeyOpensPopup` | `i` в ModeChannels → infoView != nil |
| 3 | `TestInfoPopupEscCloses` | Esc → infoView == nil, ModeChannels |
| 4 | `TestInfoPopupEnterCloses` | Enter → infoView == nil, ModeChannels |

---

## Порядок реализации

```
T1 (layout + channels panel)
  │  → новый layout, навигация по каналам, open vs selected
  └── T2 (channel switching + REST history + infinite scroll + /reload)
        │  → полная история каналов
        └── T3 (root messages only + reply count + config)
              │  → чистая лента, бейджи ⤵︎/⤴︎
              └── T4 (thread panel + /reply)
                    │  → просмотр и ответ в тред
                    └── T5 (unread badges + mark as read)
                          │  → видно где есть непрочитанные
                          └── T6 (channel search popup, Ctrl+K)
                                │  → быстрый поиск
                                └── T7 (archived channels)
                                      │  → корректная обработка недоступных
                                      └── T8 (channel info popup, i)
                                             → просмотр описания и участников
```

---

## Технические детали

### Ширина панелей

Ширина панелей управляется через конфиг (секция `[ui]`):

```
total = terminal_width
channels_width = ui.channels_width   (default: 22)
dividers       = 1                   (вертикальный разделитель)

messages_width = total - channels_width - dividers
```

Thread popup — overlay поверх layout, размеры не влияют на ширину колонок.

### Навигация (клавиши)

Tab/Shift+Tab удалены. Навигация только через горячие клавиши.

**Ctrl+B** — prefix-клавиша (tmux-style): нажать Ctrl+B, затем в течение 1 с нажать стрелку:

| Ctrl+B + | Действие |
|---|---|
| ↑ или → | → ModeMessages |
| ↓ | → ModeInput |
| ← | → ModeChannels |
| любая другая | отмена (prefix сбрасывается) |

| Клавиша | Режим | Действие |
|---|---|---|
| Ctrl+J | любой | → ModeMessages |
| Ctrl+L | любой | → ModeChannels |
| Ctrl+K | любой | открыть search popup |
| Esc | любой (кроме Help/Thread/Search) | → ModeInput |
| ↑ / ↓ / PgUp / PgDn | ModeChannels | навигация по каналам (только выбор) |
| Enter | ModeChannels | открыть канал |
| ↑ / ↓ / PgUp / PgDn | ModeMessages | навигация по сообщениям |
| Enter | ModeMessages | открыть thread popup |
| Esc | ModeMessages | → ModeInput |
| ↑ / ↓ / PgUp / PgDn | ModeThread | навигация по тредовым сообщениям |
| r | ModeThread | вставить `/reply` в input → ModeInput |
| e | ModeThread (автор) | вставить `/edit <текст>` в input → ModeInput |
| d | ModeThread (автор) | заглушка (реализация в M3) |
| Esc | ModeThread | закрыть popup → ModeMessages |
| Ctrl+B → ↓ | ModeThread | → ModeInput (popup остаётся) |
| ↑ / ↓ | ModeSearch | навигация по результатам |
| Enter | ModeSearch | открыть канал/DM, закрыть popup |
| Esc / Ctrl+C | ModeSearch | закрыть popup |
| i | ModeChannels | открыть channel info popup |
| Enter / Esc | ModeInfo | закрыть popup → ModeChannels |

### Новые `tea.Msg` типы

| Тип | Задача |
|---|---|
| `MsgChannelSelected{ ChannelID string }` | T1/T2 |
| `MsgChannelHistory{ ChannelID string; Messages []mattermost.Message; Prepend bool; Err error }` | T2 |
| `MsgChannelHistoryLoading{ ChannelID string }` | T2 |
| `MsgResetCaches{}` | утилита |
| `MsgResetDB{}` | утилита |
| `MsgThreadLoaded{ RootID string; Messages []mattermost.Message; Err error }` | T4 |
| `MsgUnreadsLoaded{ Counts map[string]int; Err error }` | T5 |
| `MsgChannelRead{ ChannelID string }` | T5 |
| `MsgSearchResults{ Channels []mattermost.Channel; Users []mattermost.User; Err error }` | T6 |
| `MsgChannelMembersLoaded{ ChannelID string; Members []mattermost.User; Err error }` | T8 |

### REST API эндпоинты (новые)

| Метод | Эндпоинт | Использование |
|---|---|---|
| GET | `/channels/{id}/posts?page=N&per_page=100` | История канала |
| GET | `/posts/{id}/thread` | Тред |
| GET | `/users/me/channels/{id}/unread` | Непрочитанные |
| POST | `/channels/{id}/members/me/view` | Пометить прочитанным |
| GET | `/channels/{id}/members` | Участники канала (T8) |

---

## Конфигурация (изменения M2)

Все новые параметры добавляются в секцию `[ui]` файла `config.toml`. Значения по умолчанию соответствуют поведению без явного конфига.

```toml
[ui]
# --- существующие (M1) ---
date_format   = "15:04"
message_limit = 100
theme         = "auto"

# --- новые в M2 ---

# Ширина боковой панели каналов (в символах)
channels_width = 22

# Ширина панели треда (в символах, только когда тред открыт)
thread_width = 40

# Отображение сообщений в панели канала:
#   "root_only" — только root-сообщения (без ответов в тред)
#   "all"       — все сообщения включая ответы (поведение M1)
channel_messages = "root_only"
```

### Изменения в `internal/config/config.go`

| Поле структуры | Тип | Ключ TOML | По умолчанию | Задача |
|---|---|---|---|---|
| `UI.ChannelsWidth` | `int` | `channels_width` | `22` | T1 |
| `UI.ThreadWidth` | `int` | `thread_width` | `40` | T4 |
| `UI.ChannelMessages` | `string` | `channel_messages` | `"root_only"` | T3 |

---

## Критерии готовности M2

- [ ] `just check` — build + vet + test + lint зелёные
- [ ] Боковая панель каналов: навигация выбор (серый) vs открытие (белый), DM смешаны, сортировка по алфавиту
- [ ] Ctrl+L → channels, Ctrl+J → messages, Ctrl+B → input, Esc → input из любого режима
- [ ] Enter на канале → история загружается из REST, правая панель показывает только этот канал
- [ ] Infinite scroll: скролл до начала → подгружаются более старые сообщения
- [ ] `/reload` работает в канале и выдаёт ошибку в «All Activity»
- [ ] Индикатор загрузки в шапке по центру при REST-запросах
- [ ] Только root-сообщения в канале (при `root_only`); бейджи `⤵︎ N` и `⤴︎` видны
- [ ] Enter на сообщении с тредом → панель треда справа; `r` вставляет `/reply`; Esc закрывает
- [ ] Счётчики непрочитанных при старте и обновляются по WS; прокрутка в дно помечает прочитанным
- [ ] Ctrl+K → search popup; фильтрует в реальном времени; Enter открывает канал
- [ ] Архивированные каналы помечены `[x]`; попытка открыть → ошибка
- [ ] `i` на канале → popup с названием, описанием и участниками; Enter/Esc закрывает
