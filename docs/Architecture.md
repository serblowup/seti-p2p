# Архитектура seti-p2p

**Документ:** описание архитектуры узла оверлейной P2P-сети и принятых
инженерных решений на этапах 1–3 курсовой работы.

---

## 1. Обзор

### 1.1. Что реализовано

`seti-p2p` — узел децентрализованной оверлейной P2P-сети поверх TCP/IP.
На этапах 1–3 реализованы:

- TCP-сервер и клиент с собственным кадрированием;
- строгая JSON-сериализация payload;
- долговременная криптографическая идентичность на Ed25519;
- сверка публичного ключа, выведенного из закрытого, с загруженным
  `identity.pub`; при несовпадении узел не стартует;
- ограниченная таблица маршрутизации Kademlia из 256 bucket;
- операции `PING`, `FIND_NODE`, `STORE`, `FIND_VALUE`;
- валидация `PONG` (`FromWire`, `NodeID` отвечающего, эхо
  `PingTimestampMs`);
- отклонение bad sender в `PING`/`FIND_NODE`/`STORE`/`FIND_VALUE`
  с `ERROR` и закрытием соединения;
- итеративный lookup с параллелизмом `ALPHA = 3`; раздельные
  состояния `discovered` / `responded` / `failed`; `failed` не
  опрашиваются повторно; условие остановки по `responded.closest(k)`;
- bootstrap через seed-узел с последующим self-lookup;
- четыре схемы начального подключения (`star`, `ring`, `tree`, `multi-seed`);
- seed self-lookup для ускорения сходимости в `multi-seed`;
- LRU-вытеснение контактов с `PING` перед заменой; фоновые
  LRU-проверки с семафором `MaxBackgroundLRUChecks=4`;
- обновление адреса контакта через `TouchOrUpdate` с подтверждением
  по `PING` и откатом на старый адрес при неудаче;
- сценарий отказа seed и серия контрольных lookup;
- автоматическая проверка невырожденности DHT (`check_non_degenerate.sh`);
- `bootstrap_schemes.sh` / `compare_bootstrap_schemes.sh` — четыре
  схемы с сохранением конфигов, дампов, `buckets.csv`, `summary.json`
  в `results/2/bootstrap-schemes/`;
- воспроизводимая сравнительная оценка форматов сериализации;
- подписанные `NodeRecord` с каноническим представлением;
- локальное хранилище DHT с персистентностью в `state_dir/dht-store.json`;
- репликация `R = 3` — три ближайших по XOR хранителя; локальная
  копия владельца ведётся отдельно (`LocalStored`);
- TTL, защита от отката `sequence_number`, переопубликование владельцем;
- псевдонимы с нормализацией и детерминированным DHT-ключом;
- сценарий отказа именно хранителя (`store_experiment.sh`);
- валидация адресов IPv4 / IPv6 / hostname через `internal/netutil`;
- `Stop()` сохраняет итоговое состояние хранилища после завершения
  фоновых публикаций.

### 1.2. Что не реализовано (границы этапов)

На этапах 1–3 **не реализованы**:

- криптографическая аутентификация канала (TLS 1.3 mutual auth либо
  собственный AKE);
- end-to-end шифрование прикладных данных;
- туннельная ретрансляция и восстановление маршрута;
- прикладные сервисы (сообщения, файлы);
- защита от Sybil/Eclipse за пределами ограниченности k-bucket;
- глобальная согласованность записей (quorum, vector clock, anti-entropy);
- NAT traversal.

Ограничения и их влияние на интерпретацию результатов описаны в §11.

### 1.3. Целевые свойства

Реализация ориентирована на следующие свойства:

1. **Децентрализация.** Ни один узел не является обязательным после
   присоединения. Seed используется только для bootstrap.
2. **Ограниченность состояния.** Каждый узел хранит не более
   `256 × k_bucket_size` контактов и не более чем свои записи + записи,
   для которых он является хранителем.
3. **Криптографически связанная идентичность.** `NodeID` детерминированно
   вычисляется из долговременного публичного ключа; при загрузке
   узел сверяет `priv.Public()` с загруженным `pub`.
4. **Воспроизводимость.** Любой эксперимент запускается одной командой
   и оставляет машиночитаемые артефакты в `results/<sprint>/`.
5. **Расширяемость.** Добавление новых типов сообщений (TUNNEL_*,
   APP_*) не требует изменения формата кадра версии 1.

---

## 2. Модульная структура

### 2.1. Дерево проекта

```text
seti-p2p/
├── cmd/
│   └── node/
│       └── main.go           # точка входа: конфиг, запуск, bootstrap,
│                             # seed self-lookup, publish-self,
│                             # put-record, get-key, get-alias
├── docs/
│   ├── Architecture.md       # этот документ
│   ├── Protocol.md           # спецификация протокола
│   ├── Node record.md        # формат подписанной адресной записи
│   ├── Serialization comparison.md
│   └── sprints/
│       ├── 1.md
│       ├── 2.md
│       └── 3.md
├── internal/
│   ├── config/               # парсинг конфигурации + env + Validate
│   ├── identity/             # Ed25519, NodeID, загрузка/сохранение ключей,
│   │                         # сверка pub/priv при load
│   ├── netutil/              # ValidateHostPort: IPv4/IPv6/hostname,
│   │                         # нормализация адресов
│   ├── protocol/             # кадр, типы, JSON-кодеки, альтернативные кодеки
│   ├── routing/              # Contact, XOR, k-bucket, RoutingTable
│   │                         # (sync.Mutex, TouchOrUpdate, addressChanged)
│   ├── transport/            # TCP-соединения, RPC-клиент,
│   │                         # WriteFrameWithDeadline
│   ├── dht/                  # итеративный FIND_NODE lookup (lookup.go)
│   │                         # итеративный FIND_VALUE (find_value.go)
│   │                         # responded vs discovered, failed
│   ├── store/                # NodeRecord (record.go)
│   │                         # локальное хранилище (store.go)
│   ├── node/                 # узел: сервер, PING/FIND_NODE/STORE/FIND_VALUE,
│   │                         # bootstrap, observeAsync/observeSync,
│   │                         # maintainBucket + lruCheckSem,
│   │                         # Lookup, Put/Get/PutAlias/GetAlias, republish
│   ├── security/             # резерв под этап 4 (AKE, TLS)
│   ├── tunnel/               # резерв под этап 5 (TUNNEL_*)
│   ├── application/          # резерв под этап 5 (APP_*)
│   ├── ui/                   # резерв под CLI/веб
│   └── experiments/          # резерв под автоматизацию экспериментов
├── scripts/
│   ├── demo.sh                    # интерактивный запуск сети + lookup
│   ├── lookup.sh                  # серия контрольных lookup + CSV
│   ├── bench.sh                   # сравнение форматов сериализации
│   ├── check_non_degenerate.sh    # автоматическая проверка невырожденности DHT
│   ├── bootstrap_schemes.sh       # 4 схемы: конфиги, дампы, buckets.csv, summary.json
│   ├── compare_bootstrap_schemes.sh # сводка в bootstrap-comparison.txt
│   └── store_experiment.sh        # эксперимент STORE / FIND_VALUE + отказ хранителя
├── results/
│   ├── 1/                   # serialization-sizes.txt, serialization-bench.txt
│   ├── 2/                   # <scheme>-lookups.*, bootstrap-schemes/
│   └── 3/                   # <scheme>-store.csv, <scheme>-store.txt
└── tests/
    ├── unit/                 # кадр, config, identity, bucket, payload,
    │                         # negative, pong_validation, routing,
    │                         # store, store_record, netutil,
    │                         # find_value_validation
    └── integration/          # e2e, negative_sender, lookup_stop,
                              # observe, observe_async, address_update,
                              # STORE/FIND_VALUE, dht_ops, alias, republish
```

Каталоги `security/`, `tunnel/`, `application/`, `ui/`, `experiments/`
на этапах 1–3 зарезервированы.

Конфигурации для экспериментов генерируются скриптами `demo.sh`,
`lookup.sh`, `bootstrap_schemes.sh`, `store_experiment.sh`
в `state-<scheme>/configs/`.

### 2.2. Слои и ответственности

```text
┌─────────────────────────────────────────────┐
│ cmd/node                                    │  CLI, сигналы, wiring,
│                                             │  seed self-lookup,
│                                             │  publish-self, get-key, get-alias
├─────────────────────────────────────────────┤
│ internal/node                               │  жизненный цикл узла,
│ server, ping/find_node/store/find_value     │  обработка RPC,
│ handlers, bootstrap, observeAsync/Sync,     │  обновление таблицы,
│ maintainBucket, Lookup,                     │  фоновые LRU-проверки,
│ Put/Get/PutAlias/GetAlias, republish        │  операции с записями
├─────────────────────────────────────────────┤
│ internal/dht                                │  итеративный lookup
│ (не имеет доступа к RoutingTable)           │  и итеративный FIND_VALUE
│ responded / discovered / failed             │
├─────────────────────────────────────────────┤
│ internal/store        internal/routing      │  NodeRecord, локальное
│                                             │  хранилище; Contact, XOR,
│                                             │  k-bucket, RoutingTable,
│                                             │  TouchOrUpdate
├─────────────────────────────────────────────┤
│ internal/identity      internal/protocol    │  Ed25519, NodeID,
│                                             │  сверка pub/priv при load,
│                                             │  кадр, JSON-кодеки
├─────────────────────────────────────────────┤
│ internal/transport     internal/netutil     │  TCP, RPC-клиент,
│                                             │  WriteFrameWithDeadline;
│                                             │  ValidateHostPort
├─────────────────────────────────────────────┤
│ internal/config                             │  парсинг конфигурации,
│                                             │  Validate, applyEnv
└─────────────────────────────────────────────┘
```

**Правила зависимостей:**

- `netutil` зависит только от стандартной библиотеки (`net`,
  `strconv`, `strings`). Используется из `store` и `config`.
- `protocol` зависит от стандартной библиотеки, от `altcodecs.go`
  (msgpack/cbor) и от `store` (для `StoreRequestPayload.Record`
  и `FindValueResponsePayload.Record`). Это осознанное отступление от
  правила этапов 1–2: с этапа 3 `protocol` становится сетевым
  представлением доменных объектов, и направление «сеть знает о
  домене» корректно. Обратного импорта нет: `store` не импортирует
  `protocol`, цикла не возникает.
- `routing` зависит от `protocol` (для `ContactWire`) и `identity`
  (для проверки `NodeID`). `RoutingTable` защищена `sync.Mutex`.
  `TouchOrUpdate` возвращает старый контакт до перезаписи адреса.
- `store` зависит от `identity` (для `ParsePublicKeyDER`,
  `VerifyNodeID`), `netutil` (для `ValidateHostPort`) и стандартной
  библиотеки. Не зависит от `protocol`, `routing`, `dht`, `node`.
- `dht` зависит от `routing`, `protocol`, `transport`, `config`,
  `store` (для `FindValueResult.Record`), но **не имеет доступа** к
  `RoutingTable`: работает с неизменяемым снимком `InitialCandidates`
  и возвращает обнаруженные контакты в `Discovered`. Внутри
  `candidateSet` отдельно ведутся `byID` (discovered) и `responded`;
  `failed` не опрашиваются повторно.
- `node` собирает всё вместе: `observeAsync`/`observeSync` —
  единственная точка обновления `RoutingTable`; `maintainBucket`
  с семафором `lruCheckSem` ограничивает число фоновых LRU-проверок;
  `node.Lookup` обрабатывает `Discovered` через `observeAsync`;
  `node.Put`/`Get`/`PutAlias`/`GetAlias` — операции с записями;
  `node.republishOnce` — переопубликование.
- `cmd/node` — единственный, кто зависит от `node`.

Это позволяет заменить любой слой (например, `transport` на QUIC или
`protocol` на CBOR) без переписывания остальных.

---

## 3. Транспорт

### 3.1. Стратегия соединений

**Принято:** клиент использует **одно TCP-соединение на RPC**:
открывает соединение, отправляет один кадр-запрос, получает один
кадр-ответ и закрывает соединение. Это упрощает освобождение ресурсов
и корреляцию запроса и ответа.

**Сервер** при этом читает кадры в цикле и способен обработать
несколько RPC в одном соединении. Текущий `RPCClient` этим не
пользуется, но такая возможность заложена в `handleConn` и пригодится
на этапах 4–5, когда появится долгоживущий защищённый канал.

**Альтернативы:**

| Альтернатива | Плюсы | Минусы |
|---|---|---|
| Одно соединение на RPC (выбрано для клиента) | Простота, изоляция ошибок, естественная корреляция | Дополнительный TCP-хендшейк на RPC |
| Долгоживущее соединение с мультиплексированием | Меньше хендшейков | Сложный код: мультиплексирование, backpressure, корреляция |
| Пул соединений на пир | Компромисс | Нужна стратегия вытеснения, дополнительная сложность |

На этапах 1–3 сообщения редкие, а сеть loopback. Цена хендшейка
(~несколько десятков микросекунд) пренебрежимо мала на фоне задержек
самого lookup (медиана 1 мс). Простота кода важнее.

**Последствия:**

- Освобождение ресурсов сводится к `defer conn.Close()`.
- Каждый RPC изолирован: таймаут одного не влияет на другие.
- Нет мультиплексирования `request_id` внутри одного соединения —
  `request_id` нужен только для сопоставления запроса и ответа в
  пределах одного открытого соединения.
- Сервер допускает несколько RPC в одном соединении, что создаёт
  задел на будущие этапы.

### 3.2. Таймауты

Все таймауты настраиваемые:

- `connect_timeout_ms` — установление TCP;
- `read_timeout_ms` — чтение кадра;
- `ping_timeout_ms` — ожидание `PONG`;
- `write_timeout_ms` — запись кадра (`SetWriteDeadline`).

Каждое чтение кадра вызывается через `SetReadDeadline`, каждая запись
через `SetWriteDeadline`. Бесконечное ожидание запрещено на уровне
архитектуры: если удалённый узел не отвечает или перестал читать,
RPC завершается ошибкой, и код возвращает управление.

### 3.3. Обработка ошибок

Каждое входящее соединение обрабатывается в отдельной горутине и
регистрируется в `Node.conns`. Ошибочный кадр от удалённого узла
приводит к:

1. Логированию причины.
2. Отправке `ERROR` (если заголовок достоверно разобран) через
   `WriteFrameWithDeadline`.
3. Закрытию **соединения**, но не процесса.

Это гарантирует, что один сбойный peer не уронит узел. Проверяется
тестами `TestServerSurvives*` в `tests/unit/negative_test.go`.

`Stop()` закрывает listener, вызывает `cancel()` на `baseCtx`
(закрывает `watchCtx`, который закрывает listener и соединения),
дожидается завершения фоновых горутин (`bgWG.Wait()` — `republishLoop`
и `expireLoop`), закрывает все активные соединения через `closeConns()`,
дожидается `connWG.Wait()` и только затем вызывает `SaveToDisk()`.
Такой порядок гарантирует, что снимок хранилища содержит последние
изменения от фоновых публикаций. Отмена контекста без `Stop()` также
закрывает listener и соединения через `watchCtx`. Проверяется
`TestStop*`, `TestStartCancel*` и `TestStopSavesFinalStoreState`.

---

## 4. Протокол и сериализация

### 4.1. Формат кадра

Фиксированный заголовок 24 байта, поля заданы в `Protocol.md`.
Такой формат выбран по трём причинам:

1. **Простота парсинга.** Все поля фиксированного размера, кроме
   `payload`. Декодер не требует lookahead-логики.
2. **Расширяемость.** Поле `flags` (2 байта) зарезервировано под
   будущую фрагментацию и признаки ответа. Поле `version` позволяет
   добавить новый формат кадра без поломки совместимости.
3. **Совместимость с TCP.** Кадр — самодостаточная единица, которую
   можно читать через `io.ReadFull` независимо от того, как ОС
   разбивает поток на сегменты.

### 4.2. Сериализация payload

**Принято:** JSON через `encoding/json` с `DisallowUnknownFields` и
проверкой `dec.More()`.

**Обоснование** подробно изложено в `Serialization comparison.md`.
Кратко:

- удобство отладки (читаемые журналы, ручное воспроизведение);
- строгая валидация «из коробки»;
- отсутствие внешних зависимостей в основном протоколе;
- экономия msgpack/CBOR (23–25 %) несущественна на payload < 1 КБ.

**Альтернативные кодеки** (`internal/protocol/altcodecs.go`) сохранены
для бенчмарка и будущего перехода при появлении больших записей.
Бенчмарк меряет **реальный путь протокола**
(`protocol.EncodePayload`, `protocol.DecodePing`,
`protocol.DecodeFindNodeResponse`), а не прямой
`json.Marshal`/`json.Unmarshal`.

### 4.3. Изоляция кодека

Кодек изолирован: `EncodePayload`/`Decode*` — единственное место, где
payload превращается в байты и обратно. Замена JSON на msgpack/CBOR
не затронет `transport`, `routing`, `dht`, `node`.

Это осознанное архитектурное решение: любой формат сериализации —
деталь реализации, не свойство протокола.

### 4.4. Новые типы сообщений этапа 3

Добавлены без повышения версии протокола:

- `0x05` `STORE_REQUEST` — публикация `NodeRecord`;
- `0x06` `STORE_RESPONSE` — подтверждение или отказ;
- `0x07` `FIND_VALUE_REQUEST` — поиск записи по ключу;
- `0x08` `FIND_VALUE_RESPONSE` — найденная запись либо ближайшие контакты.

Новые payload-схемы используют `store.NodeRecord` как вложенный объект.
Это создаёт зависимость `protocol → store`, описанную в §2.2.

---

## 5. Идентичность

### 5.1. Ed25519

**Принято:** Ed25519 как алгоритм долговременной подписи и идентичности.

**Обоснование:**

- короткие ключи (32 байта публичный, 64 байта приватный);
- быстрая подпись и верификация;
- детерминированные подписи;
- реализация в стандартной библиотеке (`crypto/ed25519`);
- рекомендован в требованиях ТЗ.

### 5.2. `NodeID = SHA-256(canonical_encode(pub))`

**Принято:** `NodeID` вычисляется как `SHA-256` от канонического
представления публичного ключа. Canonical encoding — **DER
SubjectPublicKeyInfo** (результат `x509.MarshalPKIXPublicKey`).

**Альтернативы canonical encoding:**

| Вариант | Плюсы | Минусы |
|---|---|---|
| DER SPKI (выбрано) | Стандарт, есть в stdlib | Формально «избыточен» |
| Raw 32 байта Ed25519 | Компактно | Требует явного указания алгоритма и его версии |
| PEM | Читаемо | Содержит заголовки, не годится как canonical |

DER SPKI выбран как компромисс: он самодостаточен (алгоритм внутри),
доступен в stdlib, стабилен между версиями Go.

### 5.3. Хранение ключей и сверка pub/priv

Приватный ключ хранится в PEM-файле в `state_dir` узла. Каталог
состояния индивидуален для каждого узла. Закрытый ключ никогда не
передаётся по сети, не логируется, не включается в Docker-образ.

При повторном запуске из того же `state_dir` узел восстанавливает
идентичность. Это гарантирует устойчивость `NodeID` между сессиями.

**Сверка ключа при загрузке (E2-1).** `identity.load` после парсинга
`identity.key` (PKCS#8) и `identity.pub` (SPKI) вычисляет
`derived := priv.Public().(ed25519.PublicKey)` и сравнивает его с
загруженным `pub` через `bytes.Equal`. При несовпадении —
`identity: public key does not match private key`, `LoadOrCreate`
возвращает ошибку, `node.New` падает, `main.go` завершается.

`LoadOrCreate` также различает три случая: оба файла есть → `load`;
только один есть → явная ошибка (`identity.key exists but identity.pub
missing` / наоборот); оба отсутствуют → генерация новой пары.

Покрыто `TestIdentityLoadRejectsMismatchedKeys`,
`TestIdentityLoadAcceptsMatchingKeys`, `TestIdentityLoadRejectsMissingPub`,
`TestIdentityLoadRejectsMissingPriv`,
`TestNodeNewFailsOnMismatchedIdentity`.

### 5.4. Подпись `NodeRecord`

`NodeRecord` подписывается тем же долговременным Ed25519-ключом, что
и `NodeID`. Это гарантирует, что запись с данным `NodeID` может
создать только владелец соответствующего приватного ключа.

`Verify()` проверяет `NodeID ↔ pubkey`, подпись и длину подписи.
`Validate()` дополнительно проверяет TTL, адреса (через
`netutil.ValidateHostPort`) и монотонность `sequence_number`.
Подробности — в `docs/Node record.md`.

---

## 6. Маршрутизация

### 6.1. Организация таблицы

**Принято:** 256 фиксированных bucket по индексу старшего установленного
бита XOR-расстояния.

**Альтернативы:**

| Вариант | Плюсы | Минусы |
|---|---|---|
| 256 фиксированных bucket (выбрано) | Простая реализация, детерминированная структура, простой экспорт | Большинство bucket пустые при малых `N` |
| Динамическое разбиение bucket по правилу Kademlia | Меньше пустых bucket | Сложнее: split/merge, отдельные правила |
| 1 bucket на весь k | Просто | Вырождается в плоский список |

При `N = 20` большинство из 256 bucket пусты, но это не является
проблемой: `Snapshot` пропускает пустые bucket, а сами bucket
аллоцируются лениво.

### 6.2. LRU-порядок и вытеснение

**Принято:** контакты внутри bucket упорядочены по времени последнего
наблюдения; при заполнении bucket проверяется LRU через `PING`.

**Альтернативы:**

| Вариант | Плюсы | Минусы |
|---|---|---|
| LRU + PING перед вытеснением (выбрано) | Устойчивость к временным сбоям, устойчивость к Sybil-заполнению | Дополнительный RPC на каждое вытеснение |
| Вытеснение без проверки | Проще | Живой контакт легко выбивается |
| Приоритет по расстоянию | Формально «оптимальнее» | Противоречит спецификации Kademlia |

Kademlia предписывает именно LRU-вытеснение с проверкой: живой
контакт сохраняется, мёртвый вытесняется.

**Ограничение фоновых проверок (E2-4).** `observeAsync` (вызывается из
`handlePing`, `handleFindNode`, `handleStore`, `handleFindValue`)
запускает `go maintainBucket(...)` только при `checkLRU=true` или
`addressChanged=true`. `maintainBucket` захватывает слот в
`lruCheckSem` (ёмкость `MaxBackgroundLRUChecks=4`); если семафор
занят — возвращается без проверки, LRU сохраняется, новый контакт
не добавляется. Это соответствует Kademlia: при переполненном bucket
и занятых проверках новый контакт отбрасывается. Счётчик
`lruChecksInFlight` (atomic) доступен через `LRUChecksInFlight()`
для тестов.

`observeSync` (используется `ObserveContact` в тестах) вызывает
`maintainBucketBlocking` без семафора, синхронно, чтобы тесты были
детерминированы.

### 6.3. Обновление адреса контакта (E2-7)

`Bucket.TouchOrUpdate(c)` возвращает `(updated, oldSnapshot, found)`:
- `oldSnapshot` — копия контакта **до** перезаписи `Host/Port`;
- `updated = oldSnapshot.Host != c.Host || oldSnapshot.Port != c.Port`;
- контакт перемещается в конец bucket (Touch).

`RoutingTable.Observe` возвращает `(checkLRU, lru, addressChanged,
oldContact, err)`. При `addressChanged=true` `node.observe`:
- вызывает `pingContact(c)` для подтверждения нового адреса;
- при `PONG` — `TouchVerified(c.NodeID, ...)`;
- иначе — `ReplaceIfDead(c.NodeID, oldContact)` и лог
  `observe: reverting address for <short> to <old>`.

Это защищает от MITM-подмены адреса без подтверждения. Покрыто
`TestObserveUpdatesContactAddress`,
`TestObserveKeepsOldAddressIfNewUnreachable`,
`TestNodeObserveUpdatesAddressAfterPing`,
`TestNodeObserveKeepsOldAddressIfNewUnreachable`.

### 6.4. `observeAsync` / `observeSync` как единая точка обновления

**Принято:** все обработчики входящих RPC (`handlePing`,
`handleFindNode`, `handleStore`, `handleFindValue`) и `bootstrapPing`
вызывают `observeAsync(sender)`. `node.Lookup` и `node.Get`
обрабатывают `Discovered` через `observeAsync`. `ObserveContact`
(публичный, для тестов) вызывает `observeSync`.

Модули `dht.Lookup` и `dht.FindValue` **не имеют доступа** к
`RoutingTable`: они работают с неизменяемым снимком
`InitialCandidates` и возвращают обнаруженные контакты в `Discovered`,
а `node` обрабатывает их через `observeAsync` после `wg.Wait()`.

Это даёт:

- обработчики входящих RPC не блокируются на LRU-проверке;
- единое поведение для PING, FIND_NODE, STORE, FIND_VALUE и lookup;
- единственное место для LRU-логики и обновления адреса;
- отсутствие гонок при параллельных запросах внутри lookup.

### 6.5. Потокобезопасность

**Принято:** `RoutingTable` защищена `sync.Mutex`.

Все публичные методы (`Observe`, `Promote`, `ReplaceIfDead`,
`TouchVerified`, `Nearest`, `Snapshot`, `TotalContacts`) захватывают
мьютекс. Внутренняя реализация `observeLocked` используется внутри уже
захваченного мьютекса, чтобы избежать двойного захвата.

---

## 7. DHT lookup

### 7.1. Параллелизм `ALPHA = 3`

**Принято:** `ALPHA = 3` — три параллельных `FIND_NODE_REQUEST` на
итерацию.

**Обоснование:**

- соответствует спецификации Kademlia;
- баланс между временем сходимости и нагрузкой на сеть;
- при `k = 4` за одну итерацию опрашиваются четыре ближайших контакта;
- устойчивость к одному таймауту: два других запроса продолжат поиск.

Значение вынесено в конфигурацию и может быть изменено без правки кода.

### 7.2. Состояния: discovered, responded, failed (E2-6)

`candidateSet` ведёт два отображения:

- `byID` — все **обнаруженные** контакты (из ответов);
- `responded` — контакты, которые **ответили** на наш RPC.

`failed` — отдельная map в `Run`; контакт, на который RPC завершился
таймаутом или ошибкой, помечается `failed` и больше не опрашивается
в этом lookup.

**Различие принципиально:** контакт может быть `discovered` (пришёл в
ответе), но не `responded` (мы его ещё не опрашивали или опрос
завершился ошибкой). Условие остановки и итоговый `Closest` должны
считаться по `responded`, а не по `discovered`.

### 7.3. Условие остановки

**Принято:** поиск продолжается, пока существует неопрошенный
кандидат, который ближе текущего `k`-го **ответившего**. Поиск
останавливается, когда дальнейший опрос не может улучшить список
ближайших `k` **ответивших** контактов.

`hasUnqueriedCloserThan(target, queried, failed, respondedClosest)`
сравнивает неопрошенных кандидатов с `worst` из
`respondedClosest(k)`. Если `responded` пуст — продолжает, пока есть
неопрошенные (иначе поиск останавливался бы на первой итерации).

**Альтернативы:**

| Вариант | Плюсы | Минусы |
|---|---|---|
| Остановка по «нашли цель» | Быстро для FIND_NODE | Не подходит для FIND_VALUE: нужно добирать до k ближайших |
| Остановка по «нет новых кандидатов» | Тоже корректно | Может пропустить лучших кандидатов |
| Остановка по k responded (выбрано) | Совместимо с FIND_VALUE; неопрошенные/failed не блокируют | Чуть больше RPC в редких случаях |

Выбранное условие универсально: то же самое ядро lookup используется
для `FIND_VALUE`.

### 7.4. Трассировка и `Discovered`

Каждый lookup возвращает `LookupResult` с полями `rpcs`, `iterations`,
`timeouts`, `responded`, `duration_ms`, `trace` и `Discovered`.

`Discovered` — список **новых** контактов из ответов (тех, что
впервые добавлены в `candidateSet`). Поле не сериализуется в JSON
(`json:"-"`) и используется `node.Lookup`/`node.Get` для обновления
`RoutingTable` через `observeAsync`.

`IterationTrace` содержит `Queried`, `Discovered`, `Responded` по
итерациям.

`LookupResult.Closest` — `respondedClosest(k)`, а не
`all.closest(k)`. Это важно для `FIND_VALUE`: возвращаются ближайшие
ответившие, а не формально близкие, но недоступные.

### 7.5. Итеративный `FIND_VALUE`

`internal/dht/find_value.go` реализует итеративный `FIND_VALUE` по
той же структуре, что и `Lookup`:

- `ALPHA` параллельных запросов на итерацию;
- пул кандидатов (`candidateSet`);
- `responded` / `failed` / `discovered`;
- условие остановки «нет неопрошенных ближе k-го ответившего»;
- трассировка по итерациям.

Отличия:

- отправляет `FIND_VALUE_REQUEST`, а не `FIND_NODE_REQUEST`;
- при получении `Record` в ответе вызывает `Validate(time.Now(), nil)`
  (не `Verify()`): истёкшая, с будущим `issued_at`, с плохими адресами
  или ненормализованным alias запись от удалённого хранителя **не
  принимается**;
- сохраняет запись и **продолжает** поиск до `k` ответивших, чтобы
  обнаружить более новую версию у другого хранителя;
- возвращает `FindValueResult` с полями `Key`, `Success`, `Record`,
  `Closest`, `RPCs`, `Iterations`, `Timeouts`, `Responded`,
  `DurationMs`, `Trace`, `Discovered`.

Как и `Lookup`, `FindValue` не имеет доступа к `RoutingTable`.
`node.Get` обрабатывает `Discovered` через `observeAsync`.

---

## 8. Хранение DHT

### 8.1. `NodeRecord`

Подписанная адресная запись, связывающая `NodeID` владельца с его
адресами и (опционально) псевдонимом. Формат, канонизация, подпись
и валидация описаны в `docs/Node record.md`.

Ключевые свойства:

- `NodeID = SHA-256(canonical_encode(pub))` — та же связка, что и в
  `ContactWire`;
- каноническое представление — построчное, с доменом-разделителем
  `seti-p2p/record/v1`;
- подпись Ed25519 над каноническими байтами без `signature`;
- DHT-ключ: `SHA-256("node:" || NodeID)` или
  `SHA-256("alias:" || normalize(alias))`;
- `Validate(now, prev)` проверяет подпись, TTL, адреса (через
  `netutil.ValidateHostPort`), `issued_at` и монотонность
  `sequence_number`.

### 8.2. Локальное хранилище

`internal/store/store.go` — map `hex(key) → *NodeRecord` под
`sync.RWMutex`. Методы:

- `Put(rec)` — вызывает `Validate(now, prev)`; при истёкшей `prev`
  удаляет её до проверки; заменяет запись при большем
  `sequence_number`;
- `Get(key)` — возвращает копию, если запись не истекла;
- `DeleteExpired()` — удаляет истёкшие, возвращает число;
- `Snapshot()` — копия всех записей для экспорта;
- `OwnRecords(owner)` — записи владельца (для `republish`);
- `PutUnchecked(rec)` — кладёт запись **без** `Validate`; используется
  только в тестах защиты в глубину (`TestFindValueDropsInvalidStoredRecord`);
- `SaveToDisk()` / `LoadFromDisk()` — персистентность в
  `state_dir/dht-store.json` через атомарный `os.Rename`.
  `LoadFromDisk` пропускает истёкшие и повреждённые записи.

### 8.3. Публикация (`Put`)

`node.Put(ctx, rec)`:

1. `rec.Key()`;
2. `n.store.Put(rec)` — **первым**, до `Lookup`; `res.LocalStored =
   (localErr == nil)`. Локальная копия ведётся для `republishLoop`
   и `SaveToDisk`, **не** считается репликой, если владелец не попал
   в тройку ближайших по XOR;
3. `n.Lookup(ctx, key)` — до `K` ближайших;
4. `selectReplicasByXOR(lookupRes.Closest, R)` — до `R = 3` ближайших
   по XOR, **включая владельца, если он в тройке**;
5. параллельные `STORE_REQUEST` выбранным репликам; для `self` в
   тройке — `Acks++` только если локальный `Put` прошёл;
6. `Success = acks >= StoreAckThreshold` (`StoreAckThreshold = 2`);
7. журнал `store put key=… replicas=… acks=… success=… local_stored=… duration=…ms stored_at=…`.

`StoredAt` содержит shortID подтвердивших (8 hex-символов). Это
используется в `scripts/store_experiment.sh`, чтобы убить именно
хранителя.

**Важно (Е3-4).** Владелец **всегда** кладёт запись локально
(`LocalStored=true`), если она прошла `Validate`. Но `Replicas`,
`Acks` и `StoredAt` считаются по трём ближайшим по XOR. Если владелец
**не** в тройке, `StoredAt` его не содержит, а `Replicas`/`Acks`
отражают только тройку. Это семантика из ТЗ §10.1: «репликация на
`R = 3` ближайших к ключу доступных узлах». Раньше владелец всегда
считался одной из реплик, что нарушало XOR-выбор и завышало `Acks`
для ключей, далёких от владельца. Покрыто
`TestPutOwnerLocalEvenIfNotInReplicaSet` (лог: `owner in replica
set: false (storedAt=3 replicas=3 acks=3 N=20 K=4)`).

Если `Lookup` падает, а локальный `Put` прошёл — `Put` возвращает
`(res, nil)` с `Success=false`; запись у владельца есть,
`republish` продолжает работать. Если локальный `Put` тоже упал —
возвращается ошибка.

### 8.4. Чтение (`Get`)

`node.Get(ctx, key)`:

1. `local, hasLocal := n.store.Get(key)`;
2. если `TableSize() == 0` — возвращает `local` (fallback для
   одиночного узла);
3. иначе — всегда `dht.FindValue.Run` (локальная копия не перекрывает
   удалённую);
4. если `res.Success && res.Record != nil`: `res.Record.Validate(
   time.Now(), nil)`; при успехе — `n.store.Put(res.Record)` и возврат
   `res.Record`, **если** `res.Record.SequenceNumber >=
   local.SequenceNumber`;
5. если `hasLocal` — возвращает `local` (fallback);
6. иначе — `(nil, false, nil)`.

**Важно (Е3-2).** Раньше `node.Get` возвращал локальную копию
«слепо», не проверяя, не устарела ли она. Это приводило к тому, что
хранитель старой версии возвращал её приложению, даже если в DHT уже
лежала более новая. Теперь `Get` идёт в `FindValue`, а локальная
копия используется только как fallback. Покрыто
`TestAliasLastWriterWins` (лог: `source=remote node=c2`),
`TestPutThenGetAcrossNodes` (`source=remote`),
`TestGetRejectsExpiredRemoteRecord`.

### 8.5. Переопубликование (`republish`)

`node.republishOnce` раз в `TTL/2`:

1. `store.OwnRecords(nodeID)` — все свои записи;
2. для каждой — `cloneForRepublish(prev, now, TTLSeconds)`:
   те же `NodeID`, `IdentityPublicKey`, `Addresses`, `Alias`,
   новые `IssuedAt`, `ExpiresAt`,
   `SequenceNumber = max(prev.SequenceNumber + 1, now_ms)`;
3. `Sign(n.id.Priv)`;
4. `Put(ctx, fresh)`.

`republish` **не зависит от кворума**: `Put` кладёт запись локально
первым (`LocalStored=true`), и `cloneForRepublish` обновляет
`SequenceNumber` локально даже при `Success=false`. Это важно при
малом `N` (например, `N=2`): тройка ближайших по XOR может содержать
только `self`, `Acks=1 < 2`, `Success=false`, но `SequenceNumber`
увеличен и TTL продлён. Покрыто `TestRepublishExtendsTTL`
(лог: `replicas=1 acks=1 success=false local_stored=true`, но запись
жива через 5 секунд).

Без пересборки записи `ExpiresAt` остался бы прежним, и запись
истекла бы в исходный момент, несмотря на периодические публикации.

### 8.6. Очистка (`expire`)

`node.expireLoop` раз в 30 секунд вызывает `store.DeleteExpired()` и
логирует `store expired removed=N`.

### 8.7. Псевдонимы

`node.PutAlias`:

1. `NormalizeAlias(rawAlias)`;
2. проверка `rec.NodeID == n.nodeID`;
3. проверка `rec.Alias == ""`;
4. `rec.Alias = normalized`;
5. `rec.Sign(n.id.Priv)`;
6. `Put(ctx, rec)`.

`node.GetAlias`:

1. `NormalizeAlias(rawAlias)`;
2. `store.AliasKey(normalized)`;
3. `Get(ctx, key)` — идёт в `FindValue`, локальная устаревшая копия
   не перекрывает удалённую (см. §8.4).

Ограничения псевдонимов зафиксированы в `docs/Node record.md` §10–§11:
локальный last-writer-wins, отсутствие глобальной согласованности,
отсутствие защиты от захвата.

Для теста `TestAliasLastWriterWins` добавлен
`Node.BuildSelfRecordWithSeq(seq)`: строит запись через
`BuildSelfRecord`, перезаписывает `SequenceNumber` и переподписывает.
Это даёт детерминированный `SequenceNumber` без `time.Sleep`.

---

## 9. Bootstrap

### 9.1. Seed-узел

**Принято:** первый узел запускается с пустым `bootstrap_peers`. Это
единственный случай, когда отсутствие контактов не считается ошибкой.

**Важно:** seed-узел **не является центральным каталогом**. Он не
хранит полный список участников, не выполняет роль «сервера директории».
Его роль — дать новому узлу начальный контакт, после чего узел сам
заполняет таблицу через self-lookup.

Это подтверждается экспериментом: в initial-дампе seed часто имеет
`contacts = 0` — он не знает никого, но его знают все остальные.

### 9.2. Схемы начального подключения

Поддерживаются четыре схемы, задаваемые переменной `SCHEME` в
`scripts/demo.sh`, `scripts/lookup.sh`, `scripts/bootstrap_schemes.sh`,
`scripts/store_experiment.sh`:

| Схема | Что знает узел `i` при старте |
|---|---|
| `star` | `i ≥ 2` знает только `node-01` |
| `ring` | `i ≥ 2` знает только `node-{i-1}` |
| `tree` | `i ≥ 2` знает только `node-{i/2}` (родитель в бинарном дереве) |
| `multi-seed` | seed-узлы `1..SEEDS` образуют цепочку; клиенты `i > SEEDS` знают два seed-узла (primary и secondary) |

Все схемы — **схемы начального подключения**, а не физические
топологии. В loopback-сети все узлы IP-достижимы друг для друга.

### 9.3. Особенности `multi-seed`

В `multi-seed` seed-узлы образуют цепочку: `node-02` знает `node-01`,
`node-03` знает `node-02` и т. д. Это создаёт связный граф seed-узлов.

Клиенты (`i > SEEDS`) подключаются к двум seed-узлам одновременно:

- primary: `seed_idx = (i - SEEDS - 1) % SEEDS + 1`
- secondary: `seed_idx = primary % SEEDS + 1`

Это создаёт перекрытие между разными «ветками» DHT. Без перекрытия
`multi-seed` фрагментируется на несколько независимых подграфов, и
DHT становится вырожденной.

### 9.4. Seed self-lookup

Seed-узлы (с пустым `bootstrap_peers`) выполняют self-lookup **после**
того, как к ним подключился хотя бы один клиент. Условие срабатывания
в `cmd/node/main.go`:

```go
if len(cfg.BootstrapPeers) == 0 && nd.TableSize() > 0 {
    res, err := nd.Lookup(ctx, nd.NodeID())
    ...
}
```

Это позволяет seed-узлу обнаружить остальных через своего соседа и
заполнить таблицу контактами с разными XOR-расстояниями → несколько
non-empty bucket.

### 9.5. Отказ seed

После bootstrap seed-узел может быть отключён без последствий для
ранее присоединившихся узлов. Это проверяется сценарием
`SEED_FAILURE=1` в `scripts/demo.sh`:

1. Сеть запускается, все узлы сходятся.
2. Выполняется контрольный lookup.
3. Seed-узел убивается.
4. Между двумя другими узлами выполняется повторный lookup.

Оба lookup дают `success=true`. Это доказывает, что работа сети не
зависит от доступности bootstrap-узла.

### 9.6. Проверка невырожденности и артефакты

`scripts/check_non_degenerate.sh` автоматически проверяет три критерия:

1. Не менее 80% узлов имеют `contacts < N-1`.
2. Ни у одного узла нет полного каталога (`contacts >= N-1`).
3. Не менее 80% узлов имеют ≥2 непустых bucket.

Критерии взяты из ТЗ, раздел 6. Результат сохраняется в
`results/2/<scheme>-nondegeneracy.json` (при вызове из
`bootstrap_schemes.sh` — в `results/2/bootstrap-schemes/<scheme>-nondegeneracy.json`).

Скрипт завершается с `exit 2`, если число initial-дампов не совпадает
с `NODES`: это исключает ложный PASS при неполном наборе данных.

**`scripts/bootstrap_schemes.sh` (E2-8).** Для каждой схемы:

1. Поднимает сеть `N=20, K=4, ALPHA=3`.
2. Ждёт готовности и initial-дампов.
3. Вызывает `check_non_degenerate.sh` до `rm -rf "$state"`.
4. Копирует `configs/` и `dumps/` в
   `results/2/bootstrap-schemes/<scheme>/`.
5. Строит `results/2/bootstrap-schemes/<scheme>/buckets.csv` с полями
   `node_id,total_contacts,nonempty_buckets,bucket_index,contacts_in_bucket`.
6. Пишет `results/2/bootstrap-schemes/<scheme>/summary.json` с полями
   `scheme`, `N`, `K`, `alpha`, `avg_contacts`, `avg_nonempty_buckets`,
   `nondegeneracy_pass`.

`scripts/compare_bootstrap_schemes.sh` сводит четыре `summary.json` в
`results/2/bootstrap-schemes/bootstrap-comparison.txt` (tab-separated,
выровнено `column -t`).

Результат на `N=20, K=4, ALPHA=3`: все 4 схемы `nondegeneracy_pass=true`,
avg contacts 4.65–5.05, avg non-empty buckets 2.70–2.95.

### 9.7. Bootstrap в `store_experiment.sh`

`scripts/store_experiment.sh` использует **три** bootstrap-узла для
reader'ов (`BASE_PORT`, `BASE_PORT+1`, `BASE_PORT+2`), а не один.
Это нужно, чтобы отказ одного хранителя не блокировал bootstrap
нового reader-узла. Если бы reader знал только один адрес и этот
адрес оказывался убитым хранителем, `Bootstrap` падал бы с
`all bootstrap peers failed`, и `Get` не выполнялся бы вовсе
(`rpcs=0`). Три адреса устраняют эту проблему.

**Важно (Е3-4 + `multi-seed`).** В `multi-seed` seed-узлы —
`node-01..node-04` (при `SEEDS=4`). `store_experiment.sh` исключает
из жертв только `node-01`, поэтому жертвой может стать другой
seed-узел (`node-02..node-04`). Это одновременно отказ seed-узла и
хранителя; цепочка seed-ов рвётся, и часть reader'ов не доходит до
живых хранителей. Это ограничение модели `multi-seed`, а не дефект
`FIND_VALUE`; на `star`, `ring`, `tree` seed только `node-01`,
поэтому victim 100%. Подробнее — в `docs/sprints/3.md` §10.3 и §11.2.

---

## 10. Развёртывание

### 10.1. Loopback

На этапах 1–3 используется loopback-развёртывание: все узлы слушают
`127.0.0.1` на разных портах (`9101 … 91NN`). Это позволяет
воспроизводить эксперименты без Docker.

Преимущества:

- нулевые накладные расходы на сеть;
- простота отладки;
- изоляция через `state-<scheme>/` для разных схем.

Ограничение: loopback не моделирует реальные сетевые условия
(задержки, потери, разделение). Это учитывается в интерпретации
результатов.

### 10.2. Docker Compose (задел на будущее)

В проекте присутствуют `Dockerfile` и `docker-compose.yml`. На этапах
1–3 они не используются в обязательном сценарии, но готовы для
развёртывания на этапах 5+ и для сценариев с ограниченной
IP-достижимостью (несколько Docker-сетей, `nftables`).

### 10.3. Конфигурация и валидация

Все значимые параметры вынесены в JSON-конфиг и переменные окружения
`SETI_*`. Изменение `N`, `k`, `ALPHA`, таймаутов, `ttl_seconds`
не требует правки кода — только конфигурации.

`config.Load` вызывает `applyEnv`, который возвращает ошибку при
любом нераспарсенном `SETI_*` (`SETI_ALPHA=abc`,
`SETI_LISTEN_PORT=70000`, `SETI_BOOTSTRAP_PEERS` без порта,
`SETI_BOOTSTRAP_PEERS=127.0.0.1:abc`, `SETI_TTL_SECONDS=abc`,
`SETI_K_BUCKET_SIZE=abc`, `SETI_WRITE_TIMEOUT_MS=abc`). Адреса в
`SETI_BOOTSTRAP_PEERS` проходят через `netutil.ValidateHostPort`
(IPv4, IPv6 в скобках, hostname). Затем `Config.Validate()` проверяет
диапазоны (`alpha >= 1`, `k_bucket_size >= 1`, `ttl_seconds >= 2`,
`write_timeout_ms > 0`, `max_frame_payload ∈ (0, 65536]`,
`node_id_bits == 256`, `protocol_version == 1`, непустые
`bootstrap_peers[i].host` и ненулевые порты).

Готовые конфигурации для экспериментов генерируются скриптами
`demo.sh`, `lookup.sh`, `bootstrap_schemes.sh`,
`store_experiment.sh` в `state-<scheme>/configs/`.

### 10.4. Сценарий `store_experiment.sh`

Скрипт поднимает сеть `N = 20`, каждый узел публикует свою запись
через `-publish-self`, скрипт собирает ключи из логов, убивает
**именно хранителя** (по `stored_at`), поднимает reader-узлы с
`-get-key` и собирает CSV в `results/3/<scheme>-store.csv`.

---

## 11. Ограничения и риски

### 11.1. Известные ограничения этапа 3

| Ограничение | Причина | Влияние на интерпретацию |
|---|---|---|
| Нет TLS/AKE | Сознательно отложено на этап 4 | MITM возможен; журналы не содержат секретов |
| Нет глобальной согласованности | Осознанное ограничение модели (ТЗ §11.1) | Конкурирующие `Put` могут дать разные версии на разных хранителях |
| Нет anti-entropy | Не входит в этап 3 | Если `Put` не дошёл до хранителя, он не «догонит» |
| Нет защиты от Sybil | За пределами этапов 1–3 | DHT не защищена от массового заполнения bucket |
| Нет NAT traversal | Не входит в этапы 1–3 | Работает только в loopback/LAN |
| Нет туннелей | Этап 5 | Прикладные данные не передаются |
| `multi-seed` при `KILL_REPLICA=1` | Жертвой может стать seed-узел (`node-02..node-04`) | Цепочка seed-ов рвётся; victim 15/25 (60%) — ограничение модели, не дефект `FIND_VALUE` |

**IPv6 поддерживается на этапах 1–3.** Адреса в `NodeRecord.Addresses`,
`ContactWire.Host` и `bootstrap_peers` — `host:port`, где `host` —
IPv4-литерал, IPv6-литерал **в квадратных скобках** (`[::1]:9101`)
или RFC 1123 hostname. Нормализация через `netutil.ValidateHostPort`:
IPv4 → `net.IP.To4().String()`, IPv6 → каноническая форма RFC 5952 в
скобках, hostname → lowercase. «Почти IPv4» (`127.000.000.001`,
`127.0.0.256`) отклоняется. `SelfContact` обрабатывает `::` → `::1`.

Эти ограничения **не являются дефектами**: они соответствуют
границам этапов, зафиксированным в ТЗ.

### 11.2. Угрозы валидности экспериментов

- **Loopback.** Нулевые задержки и потери, отсутствие реальных
  сетевых эффектов. Метрики времени (1 мс) не переносятся на реальные
  сети без поправки.
- **Малый `N`.** При `N = 5–12` DHT может сходиться в состояние,
  близкое к полному каталогу, но формальные критерии невырожденности
  выполняются. На `N = 20` DHT невырождена.
- **Единая машина.** Все узлы делят CPU и память, что может вносить
  шум в тайминги.
- **Случайность `NodeID`.** Каждый прогон генерирует новые ключи,
  поэтому структура XOR-распределения меняется. Это учтено: серии
  делаются на 50 наблюдений, выводы формулируются статистически.
- **`Put` с `acks < 2`.** В 20-узловой сети часть `Put` не набирает
  два подтверждения из-за таймаутов или неудачного выбора
  кандидатов. Такие записи остаются только у владельца и не
  находятся `Get` с другого узла. Это отражено в `store_experiment.sh`:
  часть провалов `Get` не связана с убийством хранителя, а связана
  именно с этим эффектом.
- **Убийство именно хранителя.** `store_experiment.sh` парсит
  `stored_at` из лога `store put` и убивает подтвердившего. Но если
  `Put` для ключа не набрал `acks >= 2`, `stored_at` содержит только
  владельца, и сценарий не находит второго хранителя для убийства.
  В этом случае скрипт печатает `WARNING: no replica found to kill`.
- **`multi-seed` при `KILL_REPLICA=1`.** Жертвой может стать
  seed-узел (`node-02..node-04`), что одновременно является отказом
  seed и хранителя. Это снижает victim до 60% в `multi-seed`, но не
  до нуля; `non_victim` остаётся 96%. Ограничение зафиксировано в
  §11.1 и `docs/sprints/3.md` §10.3.
- **`TestBackgroundLRUCheckLimited`** использует «немой» TCP-listener
  и синтетический контакт (`NodeID` от `b`, порт от listener'а),
  чтобы `pingContact` не завершался мгновенно. Это тестовая
  конструкция для проверки семафора `MaxBackgroundLRUChecks`, а не
  сценарий рабочей сети. Остальные тесты E2-4
  (`TestPingDoesNotBlockOnLRUCheck`,
  `TestObservePromotesAliveLRUAsync`,
  `TestObserveReplacesDeadLRUAsync`) используют реальные узлы.

### 11.3. Митигации

- Сравнение схем bootstrap при одинаковых `N`, `k`, `ALPHA`,
  таймаутах контролирует один фактор.
- Серии на 50 наблюдений позволяют видеть распределение, а не
  отдельные наблюдения.
- Сырые данные сохраняются в `results/<sprint>/` для воспроизводимости.
- Автоматическая проверка невырожденности (`check_non_degenerate.sh`)
  исключает субъективную оценку «достаточно ли заполнена таблица».
- `bootstrap_schemes.sh` сохраняет конфиги, дампы, `buckets.csv`,
  `summary.json` по каждой схеме — проверка воспроизводима.
- `store_experiment.sh` использует три bootstrap-узла для reader'ов,
  чтобы отказ одного хранителя не блокировал bootstrap.
- `stored_at` в логе `Put` позволяет убивать именно хранителя, а не
  случайный узел, что делает сценарий E3-5 контролируемым.
- `node.Get` идёт в `FindValue`, локальная копия используется как
  fallback — устраняет устаревшие локальные версии.
- `Stop()` дожидается `bgWG` перед `SaveToDisk()` — снимок содержит
  последние изменения.

---

## 12. Ключевые инженерные решения

Сводка решений, зафиксированных архитектурно:

| Решение | Обоснование |
|---|---|
| TCP, одно соединение на RPC (клиент) | Простота, изоляция ошибок |
| Сервер допускает несколько RPC в соединении | Задел на этапы 4–5 |
| Фиксированный 24-байтовый заголовок | Простой парсинг, расширяемость через `version`/`flags` |
| JSON payload со строгим декодированием | Удобство отладки, отсутствие внешних зависимостей |
| Бенчмарк меряет реальный путь протокола | Сравнение форматов соответствует рабочему коду |
| Ed25519 долговременная идентичность | Стандарт, stdlib, короткие ключи |
| `NodeID = SHA-256(DER SPKI)` | Самодостаточный canonical encoding |
| Сверка `priv.Public()` с загруженным `pub` при `load` | Узел не может объявлять одну identity и подписывать другой |
| `netutil.ValidateHostPort` (IPv4 + IPv6 + hostname) | Единая валидация адресов для `store` и `config`; IPv6 поддерживается в скобках |
| 256 фиксированных bucket | Детерминированная структура, простой экспорт |
| LRU с PING перед вытеснением | Соответствие спецификации Kademlia |
| `sync.Mutex` в `RoutingTable` | Потокобезопасность при параллельных lookup |
| `LastSeenMs` в `FromWire`, `LastVerifiedMs` в `observe` | Корректный LRU-порядок по времени |
| `TouchOrUpdate` возвращает старый контакт до перезаписи адреса | Откат адреса при неудачном `PING` |
| `ALPHA = 3` | Баланс сходимости и нагрузки, спецификация |
| Разделение `discovered` / `responded` / `failed` | Условие остановки не блокируется недоступными кандидатами |
| Условие остановки по `responded.closest(k)` | Совместимо с `FIND_VALUE` |
| `failed` не опрашиваются повторно | Экономия RPC и таймаутов |
| `observeAsync` + `maintainBucket` + семафор | Обработчики входящих RPC не блокируются на LRU-проверке |
| `MaxBackgroundLRUChecks=4` | Ограничение фоновых проверок, Kademlia-совместимое отбрасывание |
| Валидация `PONG` (`FromWire`, `NodeID`, эхо `PingTimestampMs`) | Защита от подмены отвечающего и повторного ответа |
| `write_timeout_ms` + `WriteFrameWithDeadline` | Узел не зависает на клиенте, который перестал читать |
| Учёт активных соединений (`Node.conns`, `connWG`) | `Stop()` закрывает соединения и ждёт обработчики |
| `baseCtx`/`cancel` + `watchCtx` | Отмена контекста закрывает listener и соединения |
| `bgWG` + порядок `cancel → bgWG.Wait → closeConns → connWG.Wait → SaveToDisk` | Снимок содержит последние изменения от фоновых публикаций |
| Seed как bootstrap, а не каталог | Децентрализация |
| Схемы `star`/`ring`/`tree`/`multi-seed` — только начальное подключение | Разделение bootstrap-графа и underlay |
| Seed self-lookup для `multi-seed` | Связность графа seed-узлов |
| `check_non_degenerate.sh` с `exit 2` при неполном наборе дампов | Защита от ложного PASS |
| `bootstrap_schemes.sh` + `compare_bootstrap_schemes.sh` | Артефакты по каждой схеме, сводная таблица в `results/2/` |
| Кодек изолирован в `protocol` | Замена формата без переписывания |
| Канонизация `NodeRecord` построчная, адреса нормализуются | Детерминизм при IPv4/IPv6/hostname |
| Домен-разделитель `seti-p2p/record/v1` | Защита от переиспользования подписи |
| `alias` в `CanonicalBytes` только при непустом значении | Отсутствие лишних байтов, стабильность подписи |
| `R = 3` — три ближайших по XOR | Требование ТЗ §10.1 |
| `LocalStored` отдельно от `Replicas`/`Acks` | Владелец хранит локально для `republish`, но не считается репликой, если не в тройке |
| `StoreAckThreshold = 2` | Баланс между доступностью и стоимостью |
| `Put` кладёт локально первым | `republish` не зависит от кворума, TTL продлевается при `N=1..2` |
| `republish` пересобирает запись | Продление TTL, иначе `ExpiresAt` не сдвигается |
| `FIND_VALUE` добирает до `k` ответивших | Возможность найти более новую версию у другого хранителя |
| `Validate(now, nil)` в `FindValue`, `handleFindValue`, `node.Get` | Истёкшая/битая/устаревшая запись не возвращается приложению |
| `node.Get` всегда идёт в `FindValue` (если `TableSize() > 0`) | Локальная устаревшая копия не перекрывает удалённую |
| `BuildSelfRecordWithSeq` для тестов | Детерминированный `SequenceNumber` без `time.Sleep` |
| Локальный last-writer-wins по `sequence_number` | Без консенсуса — единственный практичный вариант |
| Отсутствие глобальной согласованности | Осознанное ограничение модели (ТЗ §11.1) |

---

## 13. Соответствие требованиям ТЗ этапов 1–3

| Требование ТЗ | Где отражено |
|---|---|
| Логически разделённые подсистемы | §2 (дерево, слои) |
| Транспорт TCP с кадрированием | §3, §4 |
| Стратегия соединений (клиент — одно на RPC) | §3.1 |
| Таймауты записи и чтения | §3.2 |
| Учёт активных соединений, корректный `Stop()` | §3.3 |
| Идентичность без случайной генерации `NodeID` | §5 |
| Сверка `pub`/`priv` при `load` | §5.3 |
| Kademlia-lite: XOR, k-bucket, LRU | §6 |
| Потокобезопасность таблицы | §6.5 |
| Обновление адреса контакта | §6.3 |
| `observeAsync`/`observeSync` + семафор | §6.4, §6.2 |
| Итеративный поиск с `ALPHA` | §7 |
| Состояния `discovered`/`responded`/`failed` | §7.2 |
| Условие остановки по `responded.closest(k)` | §7.3 |
| Валидация `PONG` | §12, §13 (RPC) |
| Отклонение bad sender | §12, §13 (RPC) |
| Bootstrap без центрального каталога | §9 |
| Четыре bootstrap-схемы (`star`, `ring`, `tree`, `multi-seed`) | §9.2 |
| Seed self-lookup | §9.4 |
| Автоматическая проверка невырожденности | §9.6 |
| Артефакты bootstrap-схем | §9.6 |
| Развёртывание и конфигурация без правки кода | §10 |
| Валидация конфигурации и env | §10.3 |
| IPv4, IPv6, hostname в адресах | §11.1, §12 (`netutil`) |
| `NodeRecord` с канонизацией и подписью | §8.1, `docs/Node record.md` |
| `STORE` / `FIND_VALUE` | §8.3, §8.4 |
| Репликация `R = 3` — три ближайших по XOR | §8.3 |
| TTL и защита от отката | §8.2, §8.5 |
| Переопубликование владельцем | §8.5 |
| Псевдонимы с нормализацией | §8.7 |
| Отказ хранителя | §8.3, §11.2 |
| `Stop()` сохраняет итоговое состояние | §3.3, §8.5, §12 |
| Ограничения модели | §11 |
| Расширяемость под TUNNEL/APP | §2, §14 |

---

## 14. Что дальше: задел на этапы 4–5

Архитектура готова к развитию:

- **Этап 4 (защищённый канал).** Добавляется `internal/security` с
  TLS 1.3 mutual auth (упрощённый уровень) или собственный AKE
  (продвинутый). Транспорт остаётся тем же, но оборачивается в
  защищённый слой. Сервер уже умеет обрабатывать несколько RPC в
  одном соединении — это пригодится для долгоживущего защищённого
  канала. `NodeRecord.Verify()` уже проверяет подпись владельца,
  что станет основой для аутентификации на уровне записей.
- **Этап 5 (туннели).** Добавляется `internal/tunnel` с новыми типами
  `TUNNEL_BUILD`, `TUNNEL_DATA`, `TUNNEL_ACK`, `TUNNEL_CLOSE`.
  Заголовок кадра версии 1 уже содержит поле `flags`, которое
  пригодится для признаков фрагментации. `FIND_VALUE` уже умеет
  находить запись через DHT, что нужно для построения маршрута.
- **Прикладной слой.** `internal/application` получает `APP_MESSAGE`
  и `APP_FILE_CHUNK`; end-to-end шифрование укладывается поверх
  туннеля без изменения базового протокола.

Ни одно из этих расширений не требует переписывания модулей,
реализованных на этапах 1–3.