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
- как единица репликации (`R = 3` ближайших по XOR к ключу узлов);
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

**Формат `addresses`.** `host:port`, где `host` — IPv4-литерал,
IPv6-литерал **в квадратных скобках** (`[::1]:9101`) или RFC 1123
hostname. Нормализация выполняется `netutil.ValidateHostPort`:

- IPv4 приводится к канонической форме `net.IP.To4().String()`
  (убирает ведущие нули: `127.000.000.001` → **отклоняется** как
  «почти IPv4», а не как hostname);
- IPv6 приводится к канонической форме `net.IP.String()`
  (RFC 5952: `0:0:0:0:0:0:0:1` → `::1`) и оборачивается в скобки;
- hostname приводится к нижнему регистру (DNS регистронезависим);
- порт — десятичный `1..65535`.

`Validate` требует, чтобы `addresses` в записи были **уже
нормализованы**: `netutil.ValidateHostPort(a) == a`. Это аналог
требования к `alias` и защищает `CanonicalBytes` от неоднозначности
(`[::1]:9101` и `[0:0:0:0:0:0:0:1]:9101` — семантически один адрес,
но разные строки; без нормализации подпись бы различалась).

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
2. Каждый адрес нормализуется через `netutil.ValidateHostPort` и
   сортируется лексикографически **перед** формированием канонических
   байтов. Порядок в самом `NodeRecord.Addresses` может быть любым,
   но подпись от него не зависит.
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
3. каждый адрес — `netutil.ValidateHostPort(a) == a` (уже нормализован);
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
2. `n.store.Put(rec)` — **первым**, до `Lookup`;
   `res.LocalStored = (localErr == nil)`. Локальная копия ведётся для
   `republishLoop` и `SaveToDisk`, **не** считается репликой, если
   владелец не попал в тройку ближайших по XOR;
3. `Lookup(ctx, key)` — до `K` ближайших;
4. `selectReplicasByXOR(lookupRes.Closest, R)` — до `R = 3` ближайших
   по XOR, **включая владельца, если он в тройке**;
5. параллельные `STORE_REQUEST` выбранным репликам; для `self` в
   тройке — `Acks++` только если локальный `Put` прошёл;
6. `Success = acks >= StoreAckThreshold` (`StoreAckThreshold = 2`);
7. журнал `store put key=… replicas=… acks=… success=… local_stored=… duration=…ms stored_at=…`.

`stored_at` содержит shortID подтвердивших (8 hex-символов) —
используется в `scripts/store_experiment.sh`, чтобы убить именно
хранителя.

**Семантика (Е3-4):**

- `Replicas` — число выбранных хранителей из тройки ближайших по XOR
  (до `R = 3`).
- `Acks` — число подтвердивших среди выбранных; если владелец в
  тройке и его локальный `Put` прошёл, он даёт одно подтверждение.
- `LocalStored` — владелец положил запись локально (для `republish`
  и локального кэша); **не** входит в `Acks`/`Replicas`, если
  владелец не в тройке.
- `StoredAt` — контакты подтвердивших.

Если владелец **не** в тройке, `StoredAt` его не содержит, но
`LocalStored=true` и запись у владельца в `store`. Это используется
`republishLoop` и `SaveToDisk`.

Если `Lookup` падает, а локальный `Put` прошёл — `Put` возвращает
`(res, nil)` с `Success=false`; запись у владельца есть, `republish`
продолжает работать. Если локальный `Put` тоже упал — возвращается
ошибка.

**`sequence_number`:**

- при первой публикации `BuildSelfRecord` ставит
  `SequenceNumber = now.UnixMilli()`;
- при переопубликовании `cloneForRepublish` ставит
  `SequenceNumber = max(prev.SequenceNumber + 1, now.UnixMilli())`.

Это гарантирует, что новая версия строго больше предыдущей, даже
если две публикации попали в одну миллисекунду.

### 8.2. Чтение (`Get`)

`Get(ctx, key)`:

1. `local, hasLocal := n.store.Get(key)`;
2. если `TableSize() == 0` — возвращает `local` (fallback для
   одиночного узла);
3. иначе — **всегда** `dht.FindValue.Run` (локальная копия не
   перекрывает удалённую);
4. `FindValue` не останавливается на первой найденной записи: он
   добирает до `k` ближайших, чтобы обнаружить более новую версию
   у другого хранителя; каждая найденная запись проходит
   `Validate(time.Now(), nil)` — истёкшая, с будущим `issued_at`,
   с плохими адресами или ненормализованным alias **не принимается**;
5. возвращает найденную валидную запись с **наибольшим**
   `sequence_number` из собранных, если
   `res.Record.SequenceNumber >= local.SequenceNumber`;
6. если `hasLocal` — возвращает `local` (fallback);
7. иначе — `(nil, false, nil)`.

**Важно (Е3-2).** Раньше `Get` возвращал локальную копию «слепо»,
не проверяя, не устарела ли она. Это приводило к тому, что хранитель
старой версии возвращал её приложению, даже если в DHT уже лежала
более новая. Теперь `Get` идёт в `FindValue`, а локальная копия
используется только как fallback. Это устраняет устаревшие локальные
версии при наличии более новых в DHT.

### 8.3. Переопубликование (`republish`)

Владелец раз в `TTL/2` вызывает `republishOnce`:

1. `store.OwnRecords(nodeID)` — все свои записи;
2. для каждой — `cloneForRepublish(prev, now, TTLSeconds)`:
   - те же `NodeID`, `IdentityPublicKey`, `Addresses`, `Alias`;
   - новые `IssuedAt`, `ExpiresAt`;
   - `SequenceNumber = max(prev.SequenceNumber + 1, now_ms)`;
3. `Sign(n.id.Priv)` — переподпись;
4. `Put(ctx, fresh)` — публикация.

`republish` **не зависит от кворума**: `Put` кладёт запись локально
первым (`LocalStored=true`), и `cloneForRepublish` обновляет
`SequenceNumber` локально даже при `Success=false`. Это важно при
малом `N` (например, `N=2`): тройка ближайших по XOR может содержать
только `self`, `Acks=1 < 2`, `Success=false`, но `SequenceNumber`
увеличен и TTL продлён.

Без пересборки записи `ExpiresAt` остался бы прежним, и запись
истекла бы в исходный момент, несмотря на периодические публикации.

### 8.4. Очистка (`expire`)

Раз в 30 секунд `expireLoop` вызывает `store.DeleteExpired()` и
логирует число удалённых записей `store expired removed=N`.

### 8.5. `Stop()` и сохранение итогового состояния

`Stop()`:

1. `cancel()` на `baseCtx` (отменяет `acceptLoop`, `watchCtx`,
   фоновые горутины);
2. `ln.Close()` — закрывает listener;
3. `bgWG.Wait()` — дожидается `republishLoop` и `expireLoop`;
4. `closeConns()` — закрывает все активные соединения;
5. `connWG.Wait()` — дожидается `handleConn`;
6. `SaveToDisk()` — сохраняет **итоговое** состояние хранилища.

Такой порядок гарантирует, что снимок содержит последние изменения
от фоновых публикаций. Покрыто `TestStopSavesFinalStoreState`.

---

## 9. Репликация

`R = 3` — число хранителей, **три ближайших по XOR к ключу**:

- `Lookup(key)` возвращает до `K` ближайших;
- `selectReplicasByXOR(lookupRes.Closest, R)` возвращает до `R = 3`
  ближайших, **включая владельца, если он в тройке**;
- успех `Put` — при `acks >= StoreAckThreshold = 2`.

**Семантика полей `StorePutResult`:**

| Поле | Смысл |
|---|---|
| `Replicas` | Число выбранных хранителей из тройки ближайших по XOR (до `R = 3`) |
| `Acks` | Число **подтвердивших** (`Stored=true`) среди выбранных; если владелец в тройке и его локальный `Put` прошёл, он даёт одно подтверждение |
| `LocalStored` | Владелец положил запись локально (для `republish` и локального кэша); **не** входит в `Acks`/`Replicas`, если владелец не в тройке |
| `StoredAt` | Контакты подтвердивших |

В логе `Put`:

- `replicas=` = `Replicas` — сколько хранителей выбрано из тройки;
- `acks=` = `Acks` — сколько подтвердило;
- `local_stored=true/false` — положил ли владелец локально;
- `stored_at=` — shortID подтвердивших.

**Важно (Е3-4).** Владелец **всегда** кладёт запись локально
(`LocalStored=true`), если она прошла `Validate`, но `Replicas`,
`Acks` и `StoredAt` считаются по трём ближайшим по XOR. Если владелец
**не** в тройке, `StoredAt` его не содержит, а `Replicas`/`Acks`
отражают только тройку. Это семантика из ТЗ §10.1: «репликация на
`R = 3` ближайших к ключу доступных узлах».

Раньше владелец всегда считался одной из реплик, что нарушало
XOR-выбор и завышало `Acks` для ключей, далёких от владельца. Теперь
`TestPutOwnerLocalEvenIfNotInReplicaSet` подтверждает обратное:
`owner in replica set: false (storedAt=3 replicas=3 acks=3 N=20 K=4)`.

Если `acks < 2`, запись остаётся только у владельца
(`LocalStored=true`) и не считается опубликованной. `FIND_VALUE` с
другого узла её не найдёт, если владелец не входит в `k` ближайших
к ключу с точки зрения этого узла. `republish` владельца частично
компенсирует это: локальная копия обновляется каждые `TTL/2`.

---

## 10. Псевдонимы

`PutAlias(ctx, rawAlias, rec)`:

1. `NormalizeAlias(rawAlias)`;
2. проверка `rec.NodeID == n.nodeID` — нельзя публиковать псевдоним
   от чужого имени;
3. проверка `rec.Alias == ""` — нельзя перезаписывать уже
   подписанное поле;
4. `rec.Validate(time.Now(), nil)` — проверка подписи, TTL, адресов
   (через `netutil.ValidateHostPort`) до переподписи. `prev=nil`:
   rollback по `sequence_number` проверит `Put` на каждом хранителе.
   Это отклоняет мусорные записи с информативной ошибкой
   `alias: invalid record: …`;
5. `rec.Alias = normalized`;
6. `rec.Sign(n.id.Priv)` — переподпись (alias входит в
   `CanonicalBytes`);
7. `Put(ctx, rec)`.

`GetAlias(ctx, rawAlias)`:

1. `NormalizeAlias(rawAlias)`;
2. `AliasKey(normalized)` — DHT-ключ;
3. `Get(ctx, key)` — идёт в `FindValue` (если `TableSize() > 0`),
   локальная устаревшая копия не перекрывает удалённую (см. §8.2).

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
  `Get` вернёт ту версию, которую отдаст первый ответивший хранитель
  с наибольшим `sequence_number`.
- **Нет anti-entropy.** Если `Put` не дошёл до какого-то хранителя,
  у него останется старая версия или ничего. `republish` владельца
  частично компенсирует это, но не гарантирует согласованности.
- **Нет анонимности.** `NodeID` и адреса передаются в открытом виде.
- **IPv6 поддерживается** (`[addr]:port`), но **без** IPv4-mapped
  IPv6 (`::ffff:127.0.0.1`) — `net.ParseIP` вернёт IPv4-mapped
  адрес, `To4()` не nil, и адрес нормализуется в IPv4. Это
  осознанно: один адрес — одно представление.
- **`multi-seed` при `KILL_REPLICA=1`.** Жертвой может стать
  seed-узел (`node-02..node-04`), что одновременно является отказом
  seed и хранителя. Это ограничение модели `multi-seed`, а не
  дефект `FIND_VALUE`; подробнее — `docs/sprints/3.md` §10.3.

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

Тесты `tests/unit/netutil_test.go` фиксируют:

- IPv4-литералы и нормализацию ведущих нулей
  (`TestValidateHostPortIPv4`);
- IPv6-литералы в скобках и нормализацию в RFC 5952
  (`TestValidateHostPortIPv6`);
- отклонение IPv6 без скобок (`TestValidateHostPortIPv6Unbracketed`);
- hostname и приведение к нижнему регистру
  (`TestValidateHostPortHostname`);
- отклонение «почти IPv4» (`127.000.000.001`, `127.0.0.256`),
  мусора, пустых строк, битых портов
  (`TestValidateHostPortRejects`).

Интеграционные тесты:

- `tests/integration/store_test.go` — `STORE`/`FIND_VALUE` на
  двух-трёх узлах; `TestFindValueDropsInvalidStoredRecord`
  (битая подпись в локальном хранилище → `handleFindValue` отдаёт
  `contacts`, не запись);
- `tests/integration/dht_ops_test.go` — `Put`/`Get` на `N = 20, K = 4`,
  отказ реплики, `TestPutThenGetAcrossNodes` (`source=remote`),
  `TestGetAfterReplicaFailure`, `TestPutReplicasByXOR`,
  `TestPutOwnerLocalEvenIfNotInReplicaSet` (`owner in replica set:
  false`), `TestGetRejectsExpiredRemoteRecord`;
- `tests/integration/alias_test.go` — псевдонимы, last-writer-wins
  (`source=remote node=c2`), отклонение чужой записи,
  `TestPutAliasRejectsInvalidRecord`,
  `TestPutAliasRejectsInvalidAddress`;
- `tests/integration/republish_test.go` — `TestRepublishExtendsTTL`
  (при `N=2`: `replicas=1 acks=1 success=false local_stored=true`,
  но TTL продлён), `TestStopSavesFinalStoreState` (при `N=1`:
  `replicas=0 acks=0 success=false local_stored=true`, снимок
  содержит новую версию после `Stop`);
- `tests/unit/find_value_validation_test.go` —
  `TestFindValueRejectsExpiredRemoteRecord` (`FindValue` не
  принимает истёкшую запись от удалённого хранителя).