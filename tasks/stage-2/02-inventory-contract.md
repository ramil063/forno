# Задача 2.2 — контракт inventory-service: CRUD ингредиентов

- **Этап:** 2, «gRPC: контракт раньше кода»
- **Оценка:** ~1.5 часа
- **Зависимости:** задача 2.1 (модуль `proto`, buf, `make gen`) — готова
- **Артефакт:** `proto/inventory/v1/inventory.proto` + сгенерированный код
- **Готово, когда:** `make gen` проходит без замечаний buf, а `make build` собирает
  сгенерированный пакет.

## Как это делают: сервис — по ресурсу, health — отдельно

В protobuf `service` нельзя размазать по нескольким файлам — это факт. Но вопрос не в этом,
а в том, что вообще должно быть сервисом.

В больших компаниях (Google API Design Guide и AIP — их публичные правила проектирования API)
принято так:

- **Сервис называется по ресурсу, которым управляет:** `IngredientService`, а не
  «InventoryService со всем подряд». Методы стандартные: `Create`/`Get`/`List`
  (позже `Update`/`Delete`). Один ресурс — один сервис; несколько сервисов в одном пакете
  и на одном gRPC-сервере — норма: сервер регистрирует их все, клиент берёт нужный.
- **Health/Ping не живёт в доменном сервисе.** Для этого есть стандартный протокол
  `grpc.health.v1.Health` (его понимают grpc-health-probe, Kubernetes, Envoy). Свой `Ping`
  в контракте — временный костыль для дымового теста, а не часть API.

Поэтому делаем так:

1. Удаляем `ping.proto` и сгенерированные `ping.pb.go` / `ping_grpc.pb.go`: `make gen`
   не удаляет файлы, которые перестали генерироваться, и в пакете останется мусор.
2. Создаём `proto/inventory/v1/ingredient.proto` с `package inventory.v1`
   и `service IngredientService`.
3. Дымовой проверкой на этапах 2.3–2.4 будет настоящий `Create`/`Get` через grpcurl —
   это полезнее Ping: сразу проверяется сквозной путь до базы.
4. Стандартный health-сервис добавим на этапе 7 или 10 — это отдельный готовый контракт,
   писать его руками не надо.

## Что должно быть в контракте

Минимум — то, что реально умеет хранилище после задачи 1.6: создать, прочитать, перечислить.
`Update`/`Archive` не добавляем, пока для них нет методов репозитория: контракт не должен
обещать то, чего сервис не может.

```proto
service IngredientService {
  rpc CreateIngredient(CreateIngredientRequest) returns (CreateIngredientResponse);
  rpc GetIngredient(GetIngredientRequest) returns (GetIngredientResponse);
  rpc ListIngredients(ListIngredientsRequest) returns (ListIngredientsResponse);
}
message Ingredient {
  int64 id = 1;
  string name = 2;
  int64 unit_id = 3;
  optional string description = 4;
  google.protobuf.Timestamp created_at = 5;
  google.protobuf.Timestamp updated_at = 6;
  google.protobuf.Timestamp archived_at = 7;
}

message CreateIngredientRequest {
  string name = 1;
  int64 unit_id = 2;
  optional string description = 3;
}
message CreateIngredientResponse { Ingredient ingredient = 1; }

message GetIngredientRequest { int64 id = 1; }
message GetIngredientResponse { Ingredient ingredient = 1; }

message ListIngredientsRequest {
  int32 limit = 1;
  int32 offset = 2;
}
message ListIngredientsResponse { repeated Ingredient ingredients = 1; }
```

Форма может меняться — это карта, а не догма. Но решения ниже обязательны.

## Требования и объяснения

1. **`optional string description`** — не просто `string`. В proto3 обычная строка не отличает
   «поле не передали» от «передали пустую строку». `optional` даёт presence: сервис поймёт,
   что `NULL` в базе и `""` — разные вещи.
2. **Время — `google.protobuf.Timestamp`**, а не `string` и не `int64`. Строка требует
   договорённости о формате и часовом поясе, int64 — о единицах. Well-known type решает это
   за нас, а в Go превращается в `timestamppb.Timestamp`.
3. **Номера полей:** начинаются с 1, идут подряд; никогда не переиспользуй номера удалённых
   полей — вместо этого `reserved 4;` и `reserved "old_name";`. Номер — это часть провода,
   а не документация.
4. **Имена:** сообщения — PascalCase, поля — snake_case, запросы/ответы — суффиксы
   `Request`/`Response`. Пакет — `inventory.v1`; версия в пакете — это обещание совместимости.
5. **В контракте нет типов хранилища.** Никаких `NUMERIC`, `BIGINT`, «nullable»-комментариев
   про Postgres. Контракт — про то, что видит клиент, а не про то, как лежит в базе.
6. **Ответ Create возвращает `Ingredient`**, а не только `id`: клиенту почти всегда нужен
   созданный объект целиком (id + времена + нормализованные поля).
7. **Валидацию не добавляем** — она будет на этапе 2.6 через `protovalidate`. Сейчас только
   структура.

## Пагинация

`limit`/`offset` — `int32`, как в контракте; сервис на этапе 2.3 передаст их в репозиторий,
где уже есть проверки границ. В контракте опиши границы комментарием: `limit = 0` — значение
по умолчанию, максимум — 200 (как в репозитории), отрицательные значения — ошибка
`InvalidArgument`.

## Как проверю

- [ ] `make gen` — buf lint чистый, код сгенерирован
- [ ] `make build` — пакет `proto` собирается
- [ ] в контракте есть `Ingredient` со всеми семью полями схемы, включая `Timestamp`
- [ ] `description` — `optional`; времена — `google.protobuf.Timestamp`
- [ ] нет `Update`/`Archive`, которых не умеет хранилище
- [ ] `ping.proto` и его сгенерированные файлы удалены; сервис называется `IngredientService`,
      `Ping` в контракте нет
- [ ] `make fmt-diff`, `make lint` — чистые

## Вопросы к размышлению (ответь в коммите)

1. Почему контракт первичен, а не «сначала напишем сервис, потом опишем API»?
2. Чем `optional string` отличается от `string` в proto3 и почему для `description` это важно?
3. Почему `google.protobuf.Timestamp`, а не строка?
4. Что будет, если удалить поле и отдать его номер новому полю? Почему proto это «переживёт»
   молча, и чем это опасно?
5. Почему Create возвращает `Ingredient`, а не только `id`?
6. Где граница между контрактом и доменом? Почему в proto не должно быть типов PostgreSQL?
