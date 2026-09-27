# seti-p2p

Узел децентрализованной оверлейной P2P-сети поверх TCP/IP с
криптографической идентичностью, таблицей маршрутизации Kademlia и
итеративным поиском узлов.

**Курсовая работа:** «Защищённая оверлейная сеть передачи данных на
основе пиринговых протоколов».
**Текущий статус:** этапы 1–2 завершены (транспорт, кадрирование,
идентичность, Kademlia DHT, bootstrap, `PING`, `FIND_NODE`).
**Уровень:** продвинутый (`N=20`, `K=4`, четыре bootstrap-схемы).

---

## 1. Что реализовано

- TCP-сервер и клиент с собственным кадрированием (24-байтовый заголовок).
- Строгая JSON-сериализация payload с `DisallowUnknownFields`.
- Долговременная идентичность на Ed25519, `NodeID = SHA-256(DER SPKI)`.
- Ограниченная таблица маршрутизации Kademlia из 256 k-bucket.
- Операции `PING`, `PONG`, `FIND_NODE_REQUEST`, `FIND_NODE_RESPONSE`.
- Итеративный lookup с параллелизмом `ALPHA = 3`.
- Bootstrap через seed-узел с последующим self-lookup.
- Четыре схемы начального подключения: `star`, `ring`, `tree`, `multi-seed`.
- Seed self-lookup для ускорения сходимости в `multi-seed`.
- LRU-вытеснение контактов с `PING` перед заменой.
- Автоматическая проверка невырожденности DHT.
- Сценарий отказа seed.
- Серия контрольных lookup и сбор метрик в CSV.
- Сравнение форматов сериализации JSON / MessagePack / CBOR.

### Что НЕ реализовано (границы этапов 1–2)

- `STORE` / `FIND_VALUE` и подписанные `NodeRecord` (этап 3).
- TLS 1.3 / AKE и end-to-end шифрование (этап 4).
- Туннельная ретрансляция (этап 5).
- Прикладные сообщения и передача файлов (этап 5).
- Защита от Sybil/Eclipse за пределами ограниченности k-bucket.

---

## 2. Требования

- **Go** 1.27 (`go.mod`: `go 1.27.0`).
- **jq** — для разбора JSON-дампов в скриптах.
- **bash** 4+ — для скриптов демонстрации и экспериментов.
- **Linux** или **macOS** — скрипты используют `mapfile`, `shuf`,
  `date +%s%N`.
- (Опционально) **OpenSSL** — для внешней проверки сертификатов.
- (Опционально) **Docker** и **docker-compose** — не используются на
  этапах 1–2, но заготовлены для будущих этапов.

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

Ожидаемый результат: **31 unit-тестов** и **2 integration-тест**
проходят без ошибок.

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
8. Проверяет невырожденность DHT.
9. Убивает seed-узел и повторяет lookup.
10. Печатает сводку таблиц.

Остановка сети — автоматическая по `trap EXIT`.

---

## 5. Основные сценарии

### 5.1. Демонстрация на продвинутом уровне (20 узлов, K=4)

```bash
# Star
NODES=20 K=4 SCHEME=star ./scripts/demo.sh

# Ring
NODES=20 K=4 SCHEME=ring ./scripts/demo.sh

# Tree
NODES=20 K=4 SCHEME=tree ./scripts/demo.sh

# Multi-seed (4 seed-узла, клиенты знают по 2 seed-узла)
NODES=20 K=4 SCHEME=multi-seed ./scripts/demo.sh
```

Каждый прогон:

- создаёт каталог `state-<scheme>/` с конфигами, логами, дампами;
- печатает `PASS: DHT is non-degenerate` или `FAIL`;
- печатает результат lookup и post-failure lookup.

### 5.2. Серия контрольных lookup

```bash
NODES=20 K=4 SCHEME=star RUNS=30 ./scripts/lookup.sh
NODES=20 K=4 SCHEME=ring RUNS=30 ./scripts/lookup.sh
NODES=20 K=4 SCHEME=tree RUNS=30 ./scripts/lookup.sh
NODES=20 K=4 SCHEME=multi-seed RUNS=30 ./scripts/lookup.sh
```

Результаты сохраняются в:

- `results/<scheme>-lookups.csv` — построчные данные;
- `results/<scheme>-lookups.txt` — сводка с агрегатами.

### 5.3. Проверка невырожденности DHT

```bash
SCHEME=star NODES=20 ./scripts/check_non_degenerate.sh
```

Скрипт проверяет три критерия из ТЗ (раздел 6):

1. Не менее 80% узлов имеют `contacts < N-1`.
2. Ни у одного узла нет полного каталога.
3. Не менее 80% узлов имеют ≥2 непустых bucket.

Результат сохраняется в `results/<scheme>-nondegeneracy.json`.

Если число дампов не совпадает с `NODES`, скрипт завершается с
`exit 2` — это защита от ложного PASS при неполном наборе данных.

### 5.4. Сравнение форматов сериализации

```bash
./scripts/bench.sh
```

Скрипт запускает:

- `TestSerializationSizes` — размеры payload в JSON/MsgPack/CBOR;
- `BenchmarkPing_*`, `BenchmarkFindNode_*` — скорости и аллокации
  (три прогона каждый).

Результаты: `results/serialization-sizes.txt`,
`results/serialization-bench.txt`.

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
  "max_frame_payload": 65536,
  "protocol_version": 1,
  "log_level": "INFO"
}
```

| Параметр | Смысл |
|---|---|
| `state_dir` | Каталог состояния узла (ключи, логи, маркеры) |
| `listen_host`, `listen_port` | Адрес TCP-сервера |
| `bootstrap_peers` | Начальные контакты; пустой список — только для seed |
| `k_bucket_size` | Ёмкость одного k-bucket |
| `alpha` | Параллелизм итеративного lookup |
| `connect_timeout_ms` | Таймаут TCP-соединения |
| `read_timeout_ms` | Таймаут чтения кадра |
| `ping_timeout_ms` | Таймаут ожидания `PONG` |
| `max_frame_payload` | Максимальный размер payload (65536) |
| `protocol_version` | Версия протокола (1) |

Готовые конфиги для экспериментов генерируются автоматически
скриптами `scripts/demo.sh` и `scripts/lookup.sh` в каталоге
`state-<scheme>/configs/`.

### Переопределение через переменные окружения

```bash
export SETI_STATE_DIR=./state/node-01
export SETI_LISTEN_HOST=127.0.0.1
export SETI_LISTEN_PORT=9101
export SETI_BOOTSTRAP_PEERS=127.0.0.1:9101,127.0.0.1:9102
export SETI_K_BUCKET_SIZE=4
export SETI_ALPHA=3
```

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
# Положить hex целевого NodeID в файл
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
│       └── main.go               # точка входа
├── docs/
│   ├── Architecture.md           # архитектура и инженерные решения
│   ├── Protocol.md               # спецификация протокола
│   ├── Serialization comparison.md # сравнение JSON / MsgPack / CBOR
│   └── sprints/
│       ├── 1.md                  # отчёт по этапу 1
│       └── 2.md                  # отчёт по этапу 2
├── internal/
│   ├── config/                   # парсинг конфига + env
│   ├── identity/                 # Ed25519, NodeID
│   ├── protocol/                 # кадр, типы, JSON-кодеки, бенчмарки
│   ├── routing/                  # Contact, XOR, k-bucket, RoutingTable
│   ├── transport/                # TCP, RPC-клиент
│   ├── dht/                      # итеративный FIND_NODE lookup
│   ├── node/                     # узел: сервер, PING, FIND_NODE, bootstrap
│   ├── security/                 # резерв под этап 4
│   ├── tunnel/                   # резерв под этап 5
│   ├── application/              # резерв под этап 5
│   ├── ui/                       # резерв
│   └── experiments/              # резерв
├── scripts/
│   ├── demo.sh                   # запуск сети + lookup + seed failure
│   ├── lookup.sh                 # серия контрольных lookup + CSV
│   ├── bench.sh                  # сравнение форматов сериализации
│   └── check_non_degenerate.sh   # проверка невырожденности DHT
├── tests/
│   ├── unit/                     # unit-тесты
│   └── integration/              # интеграционный тест
├── .gitignore
├── go.mod
├── go.sum
└── README.md
```

Артефакты запуска (`state-*/`, `results/`, логи) в `.gitignore`.

---

## 10. Тестирование

### Unit-тесты

```bash
go test ./tests/unit/... -v
```

Покрывают:

- кадрирование: round-trip, поток 100 кадров со случайным разбиением,
  завышенная длина, неверная версия;
- сервер: выживание при ошибках (`TestServerSurvives*`);
- идентичность: устойчивость `NodeID`, соответствие ключу;
- payload: round-trip всех типов, строгая валидация;
- XOR-метрика и k-bucket: вставка, `Touch`, `LRU`, `ReplaceLRU`,
  `Remove`;
- `RoutingTable`: полный bucket с живым LRU (`Promote`), полный bucket
  с мёртвым LRU (`ReplaceIfDead`), отклонение self-контакта,
  `TouchVerified`.

Итого **31 unit-тестов**.

### Integration-тесты

```bash
go test ./tests/integration/... -v
```

`TestBootstrapAndLookup` — пять узлов, bootstrap через seed, lookup
через промежуточный узел.

`TestSeedFailureDoesNotBlockLookup` — пять узлов, отказ seed, lookup
между двумя клиентами после отказа.

### Бенчмарки

```bash
go test ./internal/protocol/ -bench . -benchmem -run '^$' -count=3
```

### Все тесты одной командой

```bash
go test ./... -count=1
```

---

## 11. Документация

- **`docs/Architecture.md`** — модульная структура, слои, ключевые
  инженерные решения, ограничения, задел на этапы 3–5.
- **`docs/Protocol.md`** — формат кадра, типы сообщений, схемы payload,
  правила `NodeID`, устройство `RoutingTable`, алгоритм lookup,
  схемы bootstrap.
- **`docs/Serialization comparison.md`** — сравнение JSON, MessagePack,
  CBOR с числами по размеру, скорости и аллокациям.
- **`docs/sprints/1.md`**, **`docs/sprints/2.md`** — отчёты по этапам
  с таблицами «требование → где реализовано».

---

## 12. Результаты этапов 1–2

### Невырожденность DHT (`N=20, K=4`)

| Схема | `contacts < N-1` | full catalog | `buckets ≥ 2` | Вердикт |
|---|---|---|---|---|
| `star` | 20/20 | 0 | 16/20 | PASS |
| `ring` | 20/20 | 0 | 17/20 | PASS |
| `tree` | 20/20 | 0 | 16/20 | PASS |
| `multi-seed` | 20/20 | 0 | 18/20 | PASS |

### Серия lookup (30 запросов на схему)

| Схема | Success | avg rpcs | avg iters | median duration |
|---|---|---|---|---|
| `star` | 30/30 | 5.70 | 1.93 | 1.00 ms |
| `ring` | 30/30 | 5.03 | 1.70 | 1.00 ms |
| `tree` | 30/30 | 5.47 | 1.83 | 1.00 ms |
| `multi-seed` | 30/30 | 5.67 | 1.90 | 1.00 ms |

Все 120 lookup завершились успешно. Диапазон RPC (3–9) и итераций
(1–3) соответствует Kademlia с `ALPHA=3`, `K=4`.

### Отказ seed

Во всех схемах после убийства `node-01` повторный lookup между
`node-02` и `node-03` даёт `success=true`.

---

## 13. Ограничения

- **Loopback-развёртывание.** Нулевые задержки, отсутствие реальных
  сетевых эффектов. Метрики времени (1 мс) не переносятся на реальные
  сети без поправки.
- **Нет TLS/AKE.** Публичный ключ передаётся в открытом виде, MITM
  возможен. Защищённый канал — предмет этапа 4.
- **Нет защиты от Sybil/Eclipse.** k-bucket ограничены, но массовое
  заполнение не блокируется. За пределами этапов 1–2.
- **Нет NAT traversal.** Работает только в loopback/LAN.
- **Случайность `NodeID`.** Каждый прогон генерирует новые ключи,
  поэтому структура XOR-распределения меняется. Учтено: серии из
  30 lookup, выводы формулируются статистически.

---

## 14. Дальнейшие этапы

- **Этап 3.** `STORE` / `FIND_VALUE`, подписанные `NodeRecord`, TTL,
  репликация `R=3`, переопубликование.
- **Этап 4.** TLS 1.3 с mutual auth (упрощённый) или собственный AKE
  на Ed25519/X25519/HKDF/AEAD (продвинутый).
- **Этап 5.** Туннельная ретрансляция (`TUNNEL_BUILD`, `TUNNEL_DATA`,
  `TUNNEL_ACK`, `TUNNEL_CLOSE`), восстановление после отказа,
  прикладные сообщения и передача файлов.

Архитектура готова к этим расширениям без переписывания базовых
модулей.