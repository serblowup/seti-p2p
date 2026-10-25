# Формат подписанной адресной записи NodeRecord

**Документ:** спецификация формата, канонического представления,
подписи, валидации и жизненного цикла записи DHT.

---

## 1. Назначение

`NodeRecord` — подписанная владельцем адресная запись, которая
хранится в DHT и находится по ключу. Запись связывает `NodeID`
владельца с его текущими сетевыми адресами и (опционально) с
человекочитаемым псевдонимом.

Запись используется:

- для разрешения `NodeID` в актуальный `host:port`;
- для разрешения псевдонима в `NodeID`;
- как единица репликации (`R = 3` ближайших к ключу узлов);
- как объект, для которого обязательны проверка подписи, TTL и
  защита от отката версии.

---

## 2. Поля

| Поле | Тип | Описание |
|---|---|---|
| `node_id` | 32 байта | `NodeID` владельца. Совпадает с `SHA-256(canonical_encode(pub))` |
| `identity_public_key` | DER SPKI | Долговременный Ed25519-ключ владельца |
| `addresses` | `[]string` | Не более `MaxAddresses = 4` адресов вида `host:port` |
| `sequence_number` | uint64 | Монотонная версия записи; защита от отката |
| `issued_at` | int64 (Unix) | Момент выпуска |
| `expires_at` | int64 (Unix) | Момент истечения TTL |
| `alias` | string, опционально | Нормализованный псевдоним |
| `signature` | 64 байта | Ed25519-подпись над каноническим представлением |

Ограничения:

- `MaxAddresses = 4`;
- `MaxAliasLen = 64`;
- `MaxClockSkewSec = 60` — допустимое опережение `issued_at`.

---

## 3. Каноническое представление

Подпись покрывает **ровно те байты**, которые возвращает
`CanonicalBytes()`. Формат — построчный, с фиксированным порядком
полей и обязательным завершающим `\n`.

```
seti-p2p/record/v1
node_id=<hex>
identity_public_key=<hex>
addresses=<addr1>,<addr2>,...
sequence_number=<decimal>
issued_at=<decimal>
expires_at=<decimal>
alias=<normalized>
```

Правила:

1. Первая строка — домен-разделитель `RecordVersion`. Это защищает
   подпись от переиспользования в другом контексте.
2. `addresses` сортируются лексикографически **перед** формированием
   канонических байтов. Порядок в самом `NodeRecord.Addresses` может
   быть любым, но подпись от него не зависит.
3. `alias` присутствует в каноническом представлении только если
   он непустой. Пустой `alias` не порождает строку `alias=`.
4. `signature` в каноническое представление **не входит**.
5. Все байтовые поля кодируются hex в нижнем регистре.
6. Все числовые поля кодируются десятичными строками без ведущих
   нулей.

Детерминированность гарантирует, что одна и та же семантическая
запись даёт одинаковые байты на любой платформе и в любой версии Go.

---

## 4. Подпись и проверка

`Sign(priv)` вычисляет `ed25519.Sign(priv, CanonicalBytes())` и
записывает результат в `Signature`.

`Verify()` выполняет:

1. `len(NodeID) == 32`;
2. `identity.ParsePublicKeyDER(IdentityPublicKey)`;
3. `identity.VerifyNodeID(NodeID, pub)` — то есть
   `SHA-256(canonical_encode(pub)) == NodeID`;
4. `len(Signature) == 64`;
5. `ed25519.Verify(pub, CanonicalBytes(), Signature)`.

`Verify()` **не проверяет** TTL, `sequence_number` и адреса: они
зависят от локального времени и состояния. Это делает `Validate()`.

---

## 5. Валидация

`Validate(now, prev)`:

1. `Verify()`;
2. `len(Addresses) > 0` и `len(Addresses) <= MaxAddresses`;
3. каждый адрес — `host:port` с непустым host и портом в `[1, 65535]`;
4. если `Alias != ""`: `NormalizeAlias(Alias) == Alias`, иначе —
   `ErrRecordInvalid`. Это защищает от записи с ненормализованным
   псевдонимом: `Key()` дал бы другой DHT-ключ, чем ожидает
   отправитель, хотя подпись была бы валидна над неканонической
   формой;
5. `ExpiresAt > now.Unix()`;
6. `IssuedAt <= now.Unix() + MaxClockSkewSec`;
7. `ExpiresAt > IssuedAt`;
8. если `prev != nil`, то `SequenceNumber >= prev.SequenceNumber`.

Условие `>=` (а не `>`) намеренно: повторная публикация той же
версии при `republish` не считается откатом.

---

## 6. DHT-ключ

`Key()` возвращает 32-байтовый ключ:

- если `alias` непустой:
  `SHA-256("alias:" || NormalizeAlias(alias))`;
- иначе:
  `SHA-256("node:" || NodeID)`.

Префиксы `"alias:"` и `"node:"` разделяют пространства ключей, чтобы
псевдоним не мог «столкнуться» с адресной записью.

Функция `AliasKey(normalized)` возвращает ключ только от
нормализованного псевдонима. Она используется в `GetAlias`, когда
записи ещё нет и нужно вычислить ключ для поиска.

---

## 7. Нормализация псевдонима

`NormalizeAlias(raw)`:

1. `strings.TrimSpace`;
2. `strings.ToLower`;
3. `len(s) <= MaxAliasLen`;
4. отсутствие управляющих символов (`< 0x20`, `0x7f`).

Пустая строка после нормализации — ошибка.

---

## 8. Жизненный цикл записи

### 8.1. Публикация (`Put`)

Владелец вызывает `Put(ctx, rec)`:

1. `rec.Key()` — DHT-ключ;
2. `Lookup(ctx, key)` — до `k` ближайших узлов;
3. `selectReplicas(self, closest, R-1)` — до двух удалённых кандидатов;
4. `store.Put(rec)` — владелец всегда **пытается** положить запись
   локально; при ошибке `Validate` (истёкший TTL, откат
   `sequence_number`, плохой адрес) запись у него не сохраняется,
   и владелец не считается выбранным хранителем в `Replicas`;
5. параллельные `STORE_REQUEST` двум удалённым кандидатам;
6. `Success = acks >= StoreAckThreshold` (`StoreAckThreshold = 2`);
7. журнал `store put key=… replicas=… acks=… success=… stored_at=…`.

`stored_at` содержит shortID подтвердивших (8 hex-символов) —
используется в `scripts/store_experiment.sh`, чтобы убить именно
хранителя.

**`sequence_number`:**

- при первой публикации `BuildSelfRecord` ставит
  `SequenceNumber = now.UnixMilli()`;
- при переопубликовании `cloneForRepublish` ставит
  `SequenceNumber = max(prev.SequenceNumber + 1, now.UnixMilli())`.

Это гарантирует, что новая версия строго больше предыдущей, даже
если две публикации попали в одну миллисекунду.

### 8.2. Чтение (`Get`)

`Get(ctx, key)`:

1. проверяет локальное хранилище;
2. иначе — итеративный `FIND_VALUE` (`internal/dht/find_value.go`);
3. `FindValue` не останавливается на первой найденной записи: он
   добирает до `k` ближайших, чтобы обнаружить более новую версию
   у другого хранителя;
4. возвращает найденную валидную запись с **наибольшим**
   `sequence_number` из собранных. При равенстве `sequence_number`
   возвращается первая найденная.

### 8.3. Переопубликование (`republish`)

Владелец раз в `TTL/2` вызывает `republishOnce`:

1. `store.OwnRecords(nodeID)` — все свои записи;
2. для каждой — `cloneForRepublish(prev, now, TTLSeconds)`:
   - те же `NodeID`, `IdentityPublicKey`, `Addresses`, `Alias`;
   - новые `IssuedAt`, `ExpiresAt`;
   - `SequenceNumber = max(prev.SequenceNumber + 1, now_ms)`;
3. `Sign(n.id.Priv)` — переподпись;
4. `Put(ctx, fresh)` — публикация.

Без пересборки записи `ExpiresAt` остался бы прежним, и запись
истекла бы в исходный момент, несмотря на периодические публикации.
Это исправлено на этапе 3 (Шаг 7.5).

### 8.4. Очистка (`expire`)

Раз в 30 секунд `expireLoop` вызывает `store.DeleteExpired()` и
логирует число удалённых записей `store expired removed=N`.

---

## 9. Репликация

`R = 3` — число хранителей, включая владельца:

- владелец (self) — всегда, если локальный `store.Put` прошёл
  `Validate`;
- до `R - 1 = 2` удалённых кандидатов из `Lookup`;
- успех `Put` — при `acks >= StoreAckThreshold = 2`.

**Семантика полей `StorePutResult`:**

| Поле | Смысл |
|---|---|
| `Replicas` | Число **выбранных** хранителей, включая владельца (владелец считается выбранным, **если локальный `store.Put` прошёл `Validate`**; при ошибке локального `Put` владелец не выбран и `Replicas` уменьшается) и до `R-1` удалённых, которым **отправлен** `STORE_REQUEST` |
| `Acks` | Число **подтвердивших** (`Stored=true`) среди выбранных |
| `StoredAt` | Контакты подтвердивших |

В логе `Put`:

- `replicas=` = `Replicas` — сколько хранителей выбрано, включая
  владельца, если его локальный `store.Put` прошёл `Validate`;
- `acks=` = `Acks` — сколько подтвердило.

Если часть удалённых кандидатов не ответила за `ReadTimeoutMs`,
`replicas > acks`. Если локальный `store.Put` у владельца упал,
`Replicas` не включает владельца, и в логе это видно как
`replicas` меньше ожидаемого `R` при `acks`, равном числу
ответивших удалённых кандидатов. `StoredAt` содержит только
подтвердивших: если `acks = 2` из трёх выбранных, `StoredAt`
содержит два shortID, а `replicas=3`.

Если `acks < 2`, запись остаётся только у владельца и не считается
опубликованной. `FIND_VALUE` с другого узла её не найдёт, если
владелец не входит в `k` ближайших к ключу с точки зрения этого
узла.

---

## 10. Псевдонимы

`PutAlias(ctx, rawAlias, rec)`:

1. `NormalizeAlias(rawAlias)`;
2. проверка `rec.NodeID == n.nodeID` — нельзя публиковать псевдоним
   от чужого имени;
3. проверка `rec.Alias == ""` — нельзя перезаписывать уже
   подписанное поле;
4. `rec.Validate(time.Now(), nil)` — проверка подписи, TTL, адресов
   до переподписи. `prev=nil`: rollback по `sequence_number`
   проверит `Put` на каждом хранителе. Это отклоняет мусорные
   записи с информативной ошибкой `alias: invalid record: …`;
5. `rec.Alias = normalized`;
6. `rec.Sign(n.id.Priv)` — переподпись (alias входит в
   `CanonicalBytes`);
7. `Put(ctx, rec)`.

`GetAlias(ctx, rawAlias)`:

1. `NormalizeAlias(rawAlias)`;
2. `AliasKey(normalized)` — DHT-ключ;
3. `Get(ctx, key)`.

Ограничения:

- псевдоним не скрывает `NodeID`: в возвращённой записи всё ещё есть
  `NodeID` и `IdentityPublicKey`;
- псевдонимы не защищены от захвата: любой узел может опубликовать
  запись по любому ключу `SHA-256("alias:" || x)`;
- разрешение конфликтов — по `sequence_number` на каждом хранителе
  отдельно (локальный last-writer-wins).

---

## 11. Ограничения модели

- **Локальный last-writer-wins.** Каждый хранитель независимо
  применяет `store.Put` и хранит ту версию, которую получил последней
  с большим `sequence_number`. Глобальной согласованности нет: если
  конкурирующие публикации дошли до разных подмножеств хранителей,
  `Get` вернёт ту версию, которую отдаст первый ответивший хранитель.
- **Нет anti-entropy.** Если `Put` не дошёл до какого-то хранителя,
  у него останется старая версия или ничего. `republish` владельца
  частично компенсирует это, но не гарантирует согласованности.
- **Нет анонимности.** `NodeID` и адреса передаются в открытом виде.

---

## 12. Тесты

Тесты `tests/unit/store_record_test.go` фиксируют:

- детерминированность канонического представления при перестановке
  адресов (`TestRecordCanonicalStable`);
- наличие домена-разделителя в первой строке
  (`TestRecordCanonicalHasVersionPrefix`);
- отсутствие строки `alias=` при пустом псевдониме
  (`TestRecordCanonicalAliasOptional`);
- валидность подписи и её поломку при изменении любого поля
  (`TestRecordSignVerify`);
- отклонение подмены `NodeID`, короткого `NodeID`, битовой порчи
  подписи (`TestRecordBadNodeID`, `TestRecordShortNodeID`);
- корректность ключей `node:` и `alias:` (`TestRecordKeyNode`,
  `TestRecordKeyAlias`);
- нормализацию псевдонима (`TestNormalizeAlias`);
- TTL, откат версии, будущий `issued_at`, число и формат адресов
  (`TestValidate*`);
- отклонение ненормализованного alias и управляющих символов
  (`TestValidateRejectsUnnormalizedAlias`,
  `TestValidateRejectsControlCharInAlias`,
  `TestValidateAcceptsNormalizedAlias`).

Интеграционные тесты:

- `tests/integration/store_test.go` — `STORE`/`FIND_VALUE` на
  двух-трёх узлах;
- `tests/integration/dht_ops_test.go` — `Put`/`Get`, отказ реплики,
  `TestPutThenGetAcrossNodes`, `TestGetAfterReplicaFailure`,
  `TestPutReplicasCountsOwner` (семантика `Replicas`);
- `tests/integration/alias_test.go` — псевдонимы, last-writer-wins,
  отклонение чужой записи, `TestPutAliasRejectsInvalidRecord`,
  `TestPutAliasRejectsInvalidAddress`;
- `tests/integration/republish_test.go` — `TestRepublishExtendsTTL`.