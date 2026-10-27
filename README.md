# seti-p2p

Узел децентрализованной оверлейной P2P-сети поверх TCP/IP с
криптографической идентичностью, таблицей маршрутизации Kademlia,
итеративным поиском узлов и подписанными записями DHT.

**Курсовая работа:** «Защищённая оверлейная сеть передачи данных на
основе пиринговых протоколов».
**Текущий статус:** этапы 1–3 завершены (транспорт, кадрирование,
идентичность, Kademlia DHT, bootstrap, `PING`, `FIND_NODE`, `STORE`,
`FIND_VALUE`, подписанные `NodeRecord`, репликация `R = 3`,
TTL, переопубликование, псевдонимы).
**Уровень:** продвинутый (`N = 20`, `K = 4`, `R = 3`, четыре
bootstrap-схемы).

---

## 1. Что реализовано

### Этапы 1–2

- TCP-сервер и клиент с собственным кадрированием (24-байтовый заголовок).
- Строгая JSON-сериализация payload с `DisallowUnknownFields`.
- Долговременная идентичность на Ed25519, `NodeID = SHA-256(DER SPKI)`.
- Сверка публичного ключа, выведенного из закрытого, с загруженным
  `identity.pub`; при несовпадении узел не стартует.
- Ограниченная таблица маршрутизации Kademlia из 256 k-bucket.
- Операции `PING`, `PONG`, `FIND_NODE_REQUEST`, `FIND_NODE_RESPONSE`.
- Валидация `PONG` (`FromWire`, `NodeID` отвечающего, эхо `PingTimestampMs`).
- Отклонение bad sender в `PING`/`FIND_NODE` с `ERROR` и закрытием соединения.
- Итеративный lookup с параллелизмом `ALPHA = 3`; отдельные состояния
  `discovered` / `responded` / `failed`; `failed` не опрашиваются повторно.
- Bootstrap через seed-узел с последующим self-lookup.
- Четыре схемы начального подключения: `star`, `ring`, `tree`, `multi-seed`.
- Seed self-lookup для ускорения сходимости в `multi-seed`.
- LRU-вытеснение контактов с `PING` перед заменой; обслуживание bucket
  вынесено в фоновые `maintainBucket` с семафором `MaxBackgroundLRUChecks=4`.
- Обновление адреса контакта через `TouchOrUpdate` с подтверждением по `PING`
  и откатом на старый адрес при неудаче.
- Автоматическая проверка невырожденности DHT.
- Сценарий отказа seed.
- Серия контрольных lookup и сбор метрик в CSV.
- Сравнение форматов сериализации JSON / MessagePack / CBOR.
- Сохранение артефактов по этапам: `results/1/`, `results/2/`, `results/3/`.

### Этап 3

- Подписанные `NodeRecord` с каноническим представлением
  (`seti-p2p/record/v1`) и Ed25519-подписью.
- Локальное хранилище DHT с персистентностью в `state_dir/dht-store.json`.
- `STORE` / `FIND_VALUE` как RPC в кадре версии 1 (`0x05–0x08`).
- Репликация `R = 3` — **три ближайших по XOR** хранителя из `Lookup`;
  владелец входит в тройку только если попал по XOR. Локальная копия
  владельца ведётся отдельно (`LocalStored`), не считается репликой,
  если владелец не в тройке. Успех при `acks >= 2`.
- TTL записей, очистка истёкших, защита от отката `sequence_number`.
- Переопубликование владельцем раз в `TTL/2` с пересборкой записи;
  не зависит от кворума (`LocalStored=true` даже при `Success=false`).
- Псевдонимы с нормализацией и детерминированным DHT-ключом
  `SHA-256("alias:" || normalized)`.
- Валидация адресов IPv4 / IPv6 / hostname через `internal/netutil`;
  IPv6 — в скобках (`[::1]:9101`), «почти IPv4» (`127.000.000.001`)
  отклоняется.
- `Validate(time.Now(), nil)` для истёкших/битых/устаревших записей
  в `dht.FindValue.Run`, `handleFindValue`, `node.Get`.
- `node.Get` всегда идёт в `FindValue` (если `TableSize() > 0`),
  локальная устаревшая копия не перекрывает удалённую.
- `Stop()`: `cancel → bgWG.Wait → closeConns → connWG.Wait → SaveToDisk`;
  снимок содержит последние изменения от фоновых публикаций.
- Сценарий отказа именно хранителя (`scripts/store_experiment.sh`).

### Что НЕ реализовано (границы этапов 1–3)

- TLS 1.3 / AKE и end-to-end шифрование (этап 4).
- Туннельная ретрансляция (этап 5).
- Прикладные сообщения и передача файлов (этап 5).
- Защита от Sybil/Eclipse за пределами ограниченности k-bucket.
- Глобальная согласованность записей (quorum, vector clock, anti-entropy).
- NAT traversal.

---

## 2. Требования

- **Go** 1.27 (`go.mod`: `go 1.27.0`).
- **jq** — для разбора JSON-дампов в скриптах
  (`demo.sh`, `lookup.sh`, `check_non_degenerate.sh`,
  `bootstrap_schemes.sh`, `compare_bootstrap_schemes.sh`,
  `store_experiment.sh`).
- **bash** 4+ — для скриптов демонстрации и экспериментов.
- **Linux** или **macOS** — скрипты используют `mapfile`, `shuf`,
  `date +%s%N`.
- (Опционально) **column** (util-linux) — для выравнивания
  `bootstrap-comparison.txt`; при отсутствии используется `cat`.
- (Опционально) **Docker** и **docker-compose** — не используются на
  этапах 1–3, но заготовлены для будущих этапов.

---

## 3. Сборка

```bash
git clone https://github.com/serblowup/seti-p2p
cd seti-p2p
go build -o bin/node ./cmd/node
```

Бинарник появится в `bin/node`.

Проверка сборки и тестов:

```bash
go build ./...
go test ./tests/unit/... -v
go test ./tests/integration/... -v
```

Ожидаемый результат: **101 unit-тест** и **42 integration-теста**
проходят без ошибок. Точные числа можно получить:

```bash
go test ./tests/unit/ -v | grep -c '^--- PASS'
go test ./tests/integration/ -v | grep -c '^--- PASS'
```

`tests/integration` на продвинутом уровне (`N = 20, K = 4`) идёт
~25–30 секунд из-за подъёма 20 узлов в `dht_ops_test.go` и
`e2e_test.go`.

---

## 4. Быстрый старт

Запустить сеть из 5 узлов по схеме `star` и выполнить контрольный
lookup:

```bash
NODES=5 K=3 SCHEME=star ./scripts/demo.sh
```

Скрипт:

1. Генерирует JSON-конфиги в `state-star/configs/`.
2. Собирает `bin/node`.
3. Запускает 5 процессов на портах `9101…9105`.
4. Ждёт маркеры готовности в `state-star/ready/`.
5. Собирает initial-дампы таблиц маршрутизации в `state-star/dump/`.
6. Выбирает пару «инициатор — цель», где цель отсутствует в таблице
   инициатора.
7. Запускает `FIND_NODE` и печатает результат.
8. Проверяет невырожденность DHT (результат в `results/2/star-nondegeneracy.json`).
9. Убивает seed-узел и повторяет lookup.
10. Печатает сводку таблиц.

Остановка сети — автоматическая по `trap EXIT`.

---

## 5. Основные сценарии

### 5.1. Демонстрация на продвинутом уровне (20 узлов, K=4)

```bash
NODES=20 K=4 SCHEME=star ./scripts/demo.sh
NODES=20 K=4 SCHEME=ring ./scripts/demo.sh
NODES=20 K=4 SCHEME=tree ./scripts/demo.sh
NODES=20 K=4 SCHEME=multi-seed ./scripts/demo.sh
```

Каждый прогон:

- создаёт каталог `state-<scheme>/` с конфигами, логами, дампами;
- печатает `PASS: DHT is non-degenerate` или `FAIL`;
- печатает результат lookup и post-failure lookup;
- сохраняет результат проверки невырожденности в
  `results/2/<scheme>-nondegeneracy.json`.

### 5.2. Серия контрольных lookup

```bash
NODES=20 K=4 SCHEME=star RUNS=30 ./scripts/lookup.sh
NODES=20 K=4 SCHEME=ring RUNS=30 ./scripts/lookup.sh
NODES=20 K=4 SCHEME=tree RUNS=30 ./scripts/lookup.sh
NODES=20 K=4 SCHEME=multi-seed RUNS=30 ./scripts/lookup.sh
```

Результаты сохраняются в:

- `results/2/<scheme>-lookups.csv` — построчные данные;
- `results/2/<scheme>-lookups.txt` — сводка с агрегатами.

### 5.3. Проверка невырожденности DHT

```bash
SCHEME=star NODES=20 ./scripts/check_non_degenerate.sh
```

Скрипт проверяет три критерия из ТЗ (раздел 6):

1. Не менее 80% узлов имеют `contacts < N-1`.
2. Ни у одного узла нет полного каталога.
3. Не менее 80% узлов имеют ≥2 непустых bucket.

Результат сохраняется в `results/2/<scheme>-nondegeneracy.json`.

Если число дампов не совпадает с `NODES`, скрипт завершается с
`exit 2` — это защита от ложного PASS при неполном наборе данных.

### 5.4. Bootstrap-схемы и артефакты

```bash
NODES=20 K=4 ALPHA=3 ./scripts/bootstrap_schemes.sh
./scripts/compare_bootstrap_schemes.sh
cat results/2/bootstrap-schemes/bootstrap-comparison.txt
```

Для каждой из четырёх схем (`star`, `ring`, `tree`, `multi-seed`)
скрипт `bootstrap_schemes.sh`:

1. Поднимает сеть `N=20, K=4, ALPHA=3`.
2. Ждёт готовности всех узлов.
3. Снимает initial-дампы таблиц маршрутизации.
4. Вызывает `check_non_degenerate.sh` (результат —
   `results/2/bootstrap-schemes/<scheme>-nondegeneracy.json`).
5. Копирует конфиги и дампы в
   `results/2/bootstrap-schemes/<scheme>/{configs,dumps}/`.
6. Строит `results/2/bootstrap-schemes/<scheme>/buckets.csv` с полями
   `node_id,total_contacts,nonempty_buckets,bucket_index,contacts_in_bucket`.
7. Пишет `results/2/bootstrap-schemes/<scheme>/summary.json` с полями
   `scheme`, `N`, `K`, `alpha`, `avg_contacts`, `avg_nonempty_buckets`,
   `nondegeneracy_pass`.

`compare_bootstrap_schemes.sh` сводит четыре `summary.json` в
`results/2/bootstrap-schemes/bootstrap-comparison.txt` (tab-separated,
выровнено `column -t`).

### 5.5. Сравнение форматов сериализации

```bash
./scripts/bench.sh
```

Скрипт запускает:

- `TestSerializationSizes` — размеры payload в JSON/MsgPack/CBOR;
- `BenchmarkPing_*`, `BenchmarkFindNode_*` — скорости и аллокации
  (три прогона каждый).

Бенчмарки JSON используют **реальный путь протокола**:
`protocol.EncodePayload`, `protocol.DecodePing`,
`protocol.DecodeFindNodeResponse`, а не прямой
`json.Marshal`/`json.Unmarshal`.

Результаты: `results/1/serialization-sizes.txt`,
`results/1/serialization-bench.txt`.

### 5.6. Эксперимент `STORE` / `FIND_VALUE`

```bash
# Одна схема
NODES=20 K=4 SCHEME=star RUNS=50 KILL_REPLICA=1 ./scripts/store_experiment.sh

# Все четыре схемы
for s in star ring tree multi-seed; do
  NODES=20 K=4 ALPHA=3 SCHEME=$s RUNS=50 KILL_REPLICA=1 ./scripts/store_experiment.sh
done
```

Скрипт:

1. Поднимает сеть `N = 20, K = 4`.
2. Каждый узел публикует свою запись через `-publish-self`.
3. Собирает ключи из логов.
4. При `KILL_REPLICA=1` находит в логе `store put` список
   подтвердивших (`stored_at`) и убивает **именно хранителя**
   (не владельца и не `node-01`). Жертва выбирается только среди
   ключей, у которых `stored_at` содержит ровно три shortID
   (`acks = 3`): после отказа остаётся хотя бы один живой
   удалённый хранитель.
5. Для каждого прогона поднимает reader-узел с `-get-key`,
   bootstrap через три первых узла (чтобы отказ хранителя не
   блокировал bootstrap).
6. Собирает CSV: `run,initiator,target_key,is_victim_key,success,seq,rpcs,iters,duration_ms`.
7. Печатает в summary строку
   `victim_key=<short> reads=<N> success=<M>` — прямое
   подтверждение E3-5 из сырых данных.

В логах `store put` появилось `local_stored=true/false`; `stored_at`
может **не** содержать владельца, если владелец не попал в тройку
ближайших по XOR.

Результаты: `results/3/<scheme>-store.csv`,
`results/3/<scheme>-store.txt`.

---

## 6. Конфигурация

Каждый узел принимает JSON-конфиг через флаг `-config`:

```json
{
  "state_dir": "./state/node-01",
  "listen_host": "127.0.0.1",
  "listen_port": 9101,
  "bootstrap_peers": [],
  "node_id_bits": 256,
  "k_bucket_size": 4,
  "alpha": 3,
  "connect_timeout_ms": 3000,
  "read_timeout_ms": 5000,
  "ping_timeout_ms": 5000,
  "write_timeout_ms": 5000,
  "max_frame_payload": 65536,
  "protocol_version": 1,
  "log_level": "INFO",
  "ttl_seconds": 180
}
```

| Параметр | Смысл |
|---|---|
| `state_dir` | Каталог состояния узла (ключи, логи, маркеры, `dht-store.json`) |
| `listen_host`, `listen_port` | Адрес TCP-сервера |
| `bootstrap_peers` | Начальные контакты; пустой список — только для seed |
| `k_bucket_size` | Ёмкость одного k-bucket |
| `alpha` | Параллелизм итеративного lookup |
| `connect_timeout_ms` | Таймаут TCP-соединения |
| `read_timeout_ms` | Таймаут чтения кадра |
| `ping_timeout_ms` | Таймаут ожидания `PONG` |
| `write_timeout_ms` | Таймаут записи кадра |
| `max_frame_payload` | Максимальный размер payload (65536) |
| `protocol_version` | Версия протокола (1) |
| `ttl_seconds` | TTL записи DHT; переопубликование раз в `TTL/2` |

Готовые конфиги для экспериментов генерируются автоматически
скриптами `scripts/demo.sh`, `scripts/lookup.sh`,
`scripts/bootstrap_schemes.sh`, `scripts/store_experiment.sh`
в каталоге `state-<scheme>/configs/`.

### Переопределение через переменные окружения

```bash
export SETI_STATE_DIR=./state/node-01
export SETI_LISTEN_HOST=127.0.0.1
export SETI_LISTEN_PORT=9101
export SETI_BOOTSTRAP_PEERS=127.0.0.1:9101,127.0.0.1:9102
export SETI_K_BUCKET_SIZE=4
export SETI_ALPHA=3
export SETI_TTL_SECONDS=180
export SETI_WRITE_TIMEOUT_MS=5000
```

Все значения `SETI_*` валидируются: нераспарсенное значение
(`SETI_ALPHA=abc`, `SETI_LISTEN_PORT=70000`, `SETI_BOOTSTRAP_PEERS`
без порта, `SETI_BOOTSTRAP_PEERS=127.0.0.1:abc` и т. п.) возвращает
ошибку из `config.Load`, узел не стартует.

Адреса в `SETI_BOOTSTRAP_PEERS` проходят через
`netutil.ValidateHostPort`: поддерживаются IPv4, IPv6 в скобках
(`[::1]:9101`), RFC 1123 hostname; «почти IPv4» (`127.000.000.001`)
и мусор отклоняются.

---

## 7. Запуск одного узла

```bash
./bin/node -config state-star/configs/node-01.json
```

Флаги `cmd/node`:

| Флаг | Назначение |
|---|---|
| `-config` | Путь к JSON-конфигу (по умолчанию — `Default()` + env) |
| `-dump-dir` | Каталог для дампов таблицы маршрутизации |
| `-dump-interval` | Интервал периодического дампа (например, `2s`) |
| `-ready-file` | Файл-маркер готовности |
| `-lookup` | Hex `NodeID` для запуска `FIND_NODE` после bootstrap |
| `-boot-delay` | Задержка перед bootstrap (по умолчанию `200ms`) |
| `-publish-self` | Построить и опубликовать свою запись через `Put` |
| `-put-record` | Hex-encoded JSON `NodeRecord` для публикации |
| `-get-key` | Hex DHT-ключ для `FIND_VALUE` |
| `-get-alias` | Псевдоним для `GetAlias` |

Пример:

```bash
# Сначала запустите demo.sh с HOLD=1, чтобы получить конфиги
NODES=2 K=4 SCHEME=star HOLD=1 ./scripts/demo.sh

# В другом терминале запустите узел вручную
./bin/node \
  -config state-star/configs/node-02.json \
  -dump-dir /tmp/dumps \
  -dump-interval 2s \
  -ready-file /tmp/node-02.ready
```

---

## 8. Триггер lookup через файловую систему

Для запуска `FIND_NODE` без перезапуска узла используется файл-триггер
в каталоге состояния:

```bash
echo -n "6acf086a..." > state-star/node-02/lookup.trigger
```

Узел раз в 200 мс проверяет файл, выполняет lookup, пишет результат
в журнал и переименовывает файл в `lookup.done`.

---

## 9. Структура проекта

```text
seti-p2p/
├── cmd/
│   └── node/
│       └── main.go
├── docs/
│   ├── Architecture.md
│   ├── Protocol.md
│   ├── Node record.md
│   ├── Serialization comparison.md
│   └── sprints/
│       ├── 1.md
│       ├── 2.md
│       └── 3.md
├── internal/
│   ├── config/
│   ├── identity/
│   ├── netutil/
│   ├── protocol/
│   ├── routing/
│   ├── transport/
│   ├── dht/
│   ├── store/
│   ├── node/
│   ├── security/          (резерв, этап 4)
│   ├── tunnel/            (резерв, этап 5)
│   ├── application/       (резерв, этап 5)
│   ├── ui/                (резерв)
│   └── experiments/       (резерв)
├── scripts/
│   ├── demo.sh
│   ├── lookup.sh
│   ├── bench.sh
│   ├── check_non_degenerate.sh
│   ├── bootstrap_schemes.sh
│   ├── compare_bootstrap_schemes.sh
│   └── store_experiment.sh
├── results/
│   ├── 1/
│   │   ├── serialization-bench.txt
│   │   └── serialization-sizes.txt
│   ├── 2/
│   │   ├── bootstrap-schemes/
│   │   │   ├── bootstrap-comparison.txt
│   │   │   ├── star/
│   │   │   ├── ring/
│   │   │   ├── tree/
│   │   │   ├── multi-seed/
│   │   │   ├── star-nondegeneracy.json
│   │   │   ├── ring-nondegeneracy.json
│   │   │   ├── tree-nondegeneracy.json
│   │   │   └── multi-seed-nondegeneracy.json
│   │   ├── star-lookups.csv
│   │   ├── ring-lookups.csv
│   │   ├── tree-lookups.csv
│   │   └── multi-seed-lookups.csv
│   └── 3/
│       ├── star-store.csv
│       ├── ring-store.csv
│       ├── tree-store.csv
│       ├── multi-seed-store.csv
│       ├── star-store.txt
│       ├── ring-store.txt
│       ├── tree-store.txt
│       └── multi-seed-store.txt
├── tests/
│   ├── unit/
│   │   ├── bucket_test.go
│   │   ├── config_test.go
│   │   ├── find_value_validation_test.go
│   │   ├── frame_test.go
│   │   ├── identity_test.go
│   │   ├── negative_test.go
│   │   ├── netutil_test.go
│   │   ├── pong_validation_test.go
│   │   ├── protocol_test.go
│   │   ├── routing_test.go
│   │   ├── store_record_test.go
│   │   └── store_test.go
│   └── integration/
│       ├── address_update_test.go
│       ├── alias_test.go
│       ├── dht_ops_test.go
│       ├── e2e_test.go
│       ├── lookup_stop_test.go
│       ├── negative_sender_test.go
│       ├── observe_async_test.go
│       ├── observe_test.go
│       ├── republish_test.go
│       └── store_test.go
├── .gitignore
├── go.mod
├── go.sum
└── README.md
```

Артефакты запуска (`state-*/`, `bin/`, `*.log`, `*.key`,
`identity.pub`, `*.tmp`, `*.swp`) в `.gitignore`.
Каталог `results/` **не исключается**: артефакты экспериментов
хранятся в репозитории.

---

## 10. Тестирование

### Unit-тесты

```bash
go test ./tests/unit/... -v
```

Покрывают:

- кадрирование: round-trip, поток 100 кадров со случайным разбиением,
  завышенная длина, неверная версия, payload переменной длины
  (`0`, `1`, `MaxFramePayload`, `MaxFramePayload+1`, 36 длин от `0`
  до `MaxFramePayload`, 200 кадров с длинами `0..199`);
- сервер: выживание при ошибках (`TestServerSurvives*`), partial header,
  bad → good на одном соединении;
- идентичность: устойчивость `NodeID`, соответствие ключу, сверка
  `priv.Public()` с загруженным `pub`, отклонение рассинхронизированных
  `identity.key`/`identity.pub`, падение `node.New`;
- адреса: `TestValidateHostPortIPv4`, `TestValidateHostPortIPv6`,
  `TestValidateHostPortIPv6Unbracketed`, `TestValidateHostPortHostname`,
  `TestValidateHostPortRejects` — IPv4/IPv6/hostname, нормализация,
  отклонение «почти IPv4» и мусора;
- payload: round-trip всех типов, строгая валидация;
- XOR-метрика и k-bucket: вставка, `Touch`, `LRU`, `ReplaceLRU`,
  `Remove`;
- `RoutingTable`: полный bucket с живым LRU (`Promote`), полный bucket
  с мёртвым LRU (`ReplaceIfDead`), отклонение self-контакта,
  `TouchVerified`, обновление адреса контакта (`TouchOrUpdate`,
  `addressChanged`, `oldContact`);
- `FromWire`: accept valid, bad DER, `NodeID` mismatch, mismatched
  key+NodeID, short/long `NodeID`, bad algorithm, empty host, zero port;
- `PONG`: валидация в `pingContact` (empty payload, zero responder,
  mismatched responder, valid pong);
- `NodeRecord`: канонизация, подпись, `Validate`;
- локальное хранилище: `Put`/`Get`, откат версии, TTL, персистентность;
- `dht.FindValue.Run`: `TestFindValueRejectsExpiredRemoteRecord` —
  истёкшая запись от удалённого хранителя не принимается;
- конфигурация: 17 подтестов `TestConfigValidateRejects` + 12
  тестов `TestConfigLoad*` (включая ошибочные `SETI_*`).

Итого **101 unit-тест**.

### Integration-тесты

```bash
go test ./tests/integration/... -v
```

Покрывают:

- `TestBootstrapAndLookup` — параметризованный: `smoke_N5_K3`
  (может скипнуть, если пара не нашлась) и `advanced_N20_K4`
  (обязательный, `rpcs >= 2`, `N=20, K=4`);
- `TestSeedFailureDoesNotBlockLookup` — пять узлов, отказ seed, lookup
  между двумя клиентами после отказа;
- `TestLookupDistinguishesDiscoveredAndResponded`,
  `TestLookupDoesNotQueryFailed`, `TestLookupStopsOnRespondedKClosest`,
  `TestLookupSuccessWithDeadIntermediate` — разделение `discovered`/
  `responded`/`failed`, условие остановки по `responded.closest(k)`;
- `TestPingRejectsBadSenderContact`, `TestFindNodeRejectsBadSenderContact`,
  `TestPingRejectsBadSenderAndClosesConn` — отклонение bad sender и
  закрытие соединения после `ERROR`;
- `TestPingDoesNotBlockOnLRUCheck`, `TestBackgroundLRUCheckLimited`,
  `TestObservePromotesAliveLRUAsync`, `TestObserveReplacesDeadLRUAsync` —
  асинхронная LRU-проверка и семафор `MaxBackgroundLRUChecks`;
- `TestObservePromotesAliveLRU`, `TestObserveReplacesDeadLRU`,
  `TestObserveTouchDoesNotPing`, `TestObserveAddToNonFullBucketDoesNotPing` —
  синхронная LRU-логика на реальных `NodeID`;
- `TestNodeObserveUpdatesAddressAfterPing`,
  `TestNodeObserveKeepsOldAddressIfNewUnreachable` — обновление и
  откат адреса контакта;
- `TestStoreThenFindValue`, `TestFindValueMissReturnsContacts`,
  `TestStoreRejectsRollbackOverNetwork`, `TestStorePersistsAcrossRestart`,
  `TestFindValueDropsInvalidStoredRecord` — `handleFindValue` не отдаёт
  битую запись, возвращает `contacts`;
- `TestPutThenGetAcrossNodes` (`source=remote`),
  `TestGetAfterReplicaFailure`, `TestPutLocalOnlyOnThreeNodes`,
  `TestPutReplicasByXOR`, `TestPutOwnerLocalEvenIfNotInReplicaSet`
  (`owner in replica set: false`, `N=20, K=4`),
  `TestGetRejectsExpiredRemoteRecord` — `Put`/`Get` на продвинутом
  уровне;
- `TestPutAliasThenGetByAlias`, `TestAliasLastWriterWins`
  (`source=remote node=c2`), `TestPutAliasRejects*`, `TestGetAlias*`;
- `TestRepublishExtendsTTL` (при `N=2`: `replicas=1 acks=1 success=false
  local_stored=true`, но TTL продлён), `TestStopSavesFinalStoreState`
  (при `N=1`: `replicas=0 acks=0 success=false local_stored=true`,
  снимок содержит новую версию после `Stop`).

Итого **42 integration-теста**.

### Бенчмарки

```bash
go test ./internal/protocol/ -bench . -benchmem -run '^$' -count=3
```

### Все тесты одной командой

```bash
go test ./... -count=1
```

При `go test -short ./tests/integration/...` пропускаются длинные
тесты (`TestRepublishExtendsTTL`, `TestStopSavesFinalStoreState`),
но `dht_ops_test.go` и `e2e_test.go` **всегда** идут с `N = 20, K = 4`
(без `-short`-вариантов).

---

## 11. Документация

- **`docs/Architecture.md`** — модульная структура, слои, ключевые
  инженерные решения, ограничения, задел на этапы 4–5.
- **`docs/Protocol.md`** — формат кадра, типы сообщений, схемы payload,
  правила `NodeID`, устройство `RoutingTable`, алгоритм lookup,
  схемы bootstrap, `STORE`/`FIND_VALUE`, `NodeRecord`, IPv6 в
  адресах, `LocalStored`.
- **`docs/Node record.md`** — формат подписанной адресной записи,
  канонизация, подпись, TTL, псевдонимы, ограничения модели.
- **`docs/Serialization comparison.md`** — сравнение JSON, MessagePack,
  CBOR с числами по размеру, скорости и аллокациям
  (`results/1/serialization-*.txt`).
- **`docs/sprints/1.md`**, **`docs/sprints/2.md`**,
  **`docs/sprints/3.md`** — отчёты по этапам с таблицами
  «требование → где реализовано».

---

## 12. Результаты этапов 1–2

### Невырожденность DHT (`N=20, K=4, ALPHA=3`)

| Схема | `contacts < N-1` | full catalog | `buckets ≥ 2` | avg contacts | avg non-empty buckets | Вердикт |
|---|---|---|---|---|---|---|
| `star` | 20/20 | 0 | 18/20 | 4.65 | 2.85 | PASS |
| `ring` | 20/20 | 0 | 17/20 | 5.05 | 2.70 | PASS |
| `tree` | 20/20 | 0 | 16/20 | 4.85 | 2.95 | PASS |
| `multi-seed` | 20/20 | 0 | 18/20 | 5.05 | 2.85 | PASS |

Артефакты: `results/2/bootstrap-schemes/bootstrap-comparison.txt`,
`results/2/bootstrap-schemes/<scheme>/{configs,dumps,buckets.csv,summary.json}`,
`results/2/bootstrap-schemes/<scheme>-nondegeneracy.json`.

### Серия lookup (30 запросов на схему)

| Схема | Success | avg rpcs | avg iters | median duration |
|---|---|---|---|---|
| `star` | 30/30 | 5.70 | 1.93 | 1.00 ms |
| `ring` | 30/30 | 5.03 | 1.70 | 1.00 ms |
| `tree` | 30/30 | 5.47 | 1.83 | 1.00 ms |
| `multi-seed` | 30/30 | 5.67 | 1.90 | 1.00 ms |

Все 120 lookup завершились успешно. Диапазон RPC (3–9) и итераций
(1–3) соответствует Kademlia с `ALPHA=3`, `K=4`.
Артефакты: `results/2/<scheme>-lookups.csv`,
`results/2/<scheme>-lookups.txt`.

### Отказ seed

Во всех схемах после убийства `node-01` повторный lookup между
`node-02` и `node-03` даёт `success=true`.

---

## 13. Результаты этапа 3

### `store_experiment.sh` на `N=20, K=4, R=3, KILL_REPLICA=1, RUNS=50`

| Схема | Success | avg rpcs | avg iters | avg duration | median duration | `victim_key` reads / success | `non_victim` reads / success |
|---|---:|---:|---:|---:|---:|---|---|
| `star` | 49/50 | 7.02 | 2.49 | 1.20 ms | 1.00 ms | 27 / 27 (100%) | 23 / 22 (96%) |
| `ring` | 49/50 | 7.90 | 2.80 | 1.18 ms | 1.00 ms | 25 / 25 (100%) | 25 / 24 (96%) |
| `tree` | 50/50 | 7.12 | 2.56 | 1.12 ms | 1.00 ms | 26 / 26 (100%) | 24 / 24 (100%) |
| `multi-seed` | 39/50 | 7.95 | 2.74 | 1.41 ms | 2.00 ms | 25 / 15 (60%) | 25 / 24 (96%) |

Столбец `is_victim_key` в CSV (`1`, если `target_key == VICTIM_KEY`)
позволяет автоматически подсчитать, сколько раз читался ключ убитого
хранителя и сколько из этих чтений были успешными. В summary
печатается строка `victim_key=<short> reads=<N> success=<M>`.

Артефакты: `results/3/<scheme>-store.csv`,
`results/3/<scheme>-store.txt`.

### `TestRepublishExtendsTTL`

`TTL = 4 с`, `republishInterval = 2 с`. Через 5 секунд после
`PutLocal` запись жива, `ExpiresAt` сдвинут вперёд, `SequenceNumber`
увеличен. При `N=2` лог показывает `replicas=1 acks=1 success=false
local_stored=true` — второй узел не попал в тройку ближайших по XOR,
но `LocalStored=true`, `SequenceNumber` обновлён локально, TTL продлён.
Тест занимает ~5.0 секунды, при `go test -short` пропускается.

### `TestStopSavesFinalStoreState`

Узел с `TTL = 4 с`, `republishInterval = 2 с`. Через 3 секунды после
`PutLocal` `republishOnce` срабатывает, `SequenceNumber` увеличивается.
Затем `Stop()`: `cancel → bgWG.Wait → closeConns → connWG.Wait →
SaveToDisk`. После рестарта из того же `StateDir` `StoreGet` возвращает
**последнюю** версию. При `N=1` лог показывает `replicas=0 acks=0
success=false local_stored=true` — тройка пуста, но `LocalStored=true`
и снимок содержит новую версию.

### Псевдонимы

- `PutAlias` / `GetAlias` на `N = 20`: `alice` → `ALICE` → `alice`,
  `source=remote`.
- `TestAliasLastWriterWins` подтверждает локальный last-writer-wins
  на стороне хранителя: `sequence_number` 1 → 2 вытесняет
  предыдущую версию, `GetAlias` возвращает `c2` (`source=remote`),
  а не устаревшую локальную копию `seed` от `c1`.
- `PutAlias` отклоняет чужую запись (`ErrAliasNotOwn`), запись
  с уже установленным `alias` (`ErrAliasAlreadySet`) и запись,
  не прошедшую `Validate` (`alias: invalid record: …`).

---

## 14. Ограничения

- **Loopback-развёртывание.** Нулевые задержки, отсутствие реальных
  сетевых эффектов. Метрики времени (1 мс) не переносятся на реальные
  сети без поправки.
- **Нет TLS/AKE.** Публичный ключ передаётся в открытом виде, MITM
  возможен. Защищённый канал — предмет этапа 4.
- **Нет глобальной согласованности.** Локальный last-writer-wins по
  `sequence_number` на каждом хранителе; anti-entropy отсутствует.
  Если `Put` не дошёл до хранителя, он не «догонит».
- **Нет защиты от Sybil/Eclipse.** k-bucket ограничены, но массовое
  заполнение не блокируется. За пределами этапов 1–3.
- **Нет защиты от захвата псевдонимов.** Любой узел может опубликовать
  запись по любому ключу `SHA-256("alias:" || x)`.
- **Нет NAT traversal.** Работает только в loopback/LAN.
- **Случайность `NodeID`.** Каждый прогон генерирует новые ключи,
  поэтому структура XOR-распределения меняется. Учтено: серии из
  50 наблюдений, выводы формулируются статистически.
- **`TestBackgroundLRUCheckLimited`** использует «немой» TCP-listener
  и синтетический контакт (`NodeID` от `b`, порт от listener'а),
  чтобы `pingContact` не завершался мгновенно. Это тестовая
  конструкция для проверки семафора `MaxBackgroundLRUChecks`, а не
  сценарий рабочей сети. Остальные тесты E2-4 (`TestPingDoesNotBlockOnLRUCheck`,
  `TestObservePromotesAliveLRUAsync`, `TestObserveReplacesDeadLRUAsync`)
  используют реальные узлы.
- **IPv6 поддерживается** в скобках (`[::1]:9101`); unbracketed IPv6
  (`::1:9101`) отклоняется, IPv4-mapped IPv6 (`::ffff:127.0.0.1`)
  нормализуется в IPv4.

---

## 15. Дальнейшие этапы

- **Этап 4.** TLS 1.3 с mutual auth (упрощённый) или собственный AKE
  на Ed25519/X25519/HKDF/AEAD (продвинутый).
- **Этап 5.** Туннельная ретрансляция (`TUNNEL_BUILD`, `TUNNEL_DATA`,
  `TUNNEL_ACK`, `TUNNEL_CLOSE`), восстановление после отказа,
  прикладные сообщения и передача файлов.

Архитектура готова к этим расширениям без переписывания базовых
модулей.