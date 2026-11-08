# Задача 2.3 — gRPC-сервер inventory-service поверх репозитория

- **Этап:** 2, «gRPC: контракт раньше кода»
- **Оценка:** ~2 часа
- **Зависимости:** задачи 2.1–2.2 (контракт и генерация) — готовы
- **Артефакт:** `services/inventory-service/internal/grpcserver/server.go` (+ тесты)
  и правка `cmd/main.go`
- **Готово, когда:** сервис поднимает gRPC-сервер, а методы `CreateIngredient`,
  `GetIngredient`, `ListIngredients` работают поверх репозитория. Сетевую проверку
  через `grpcurl` сделаем в следующей задаче.

## Файлы

```
services/inventory-service/
  internal/grpcserver/server.go        сервер, методы, конвертеры row → proto
  internal/grpcserver/server_test.go   тесты без сети
  cmd/main.go                          wiring: пул → репозиторий → gRPC → shutdown
```

Пакет называется `grpcserver`, а не `grpc`: иначе он перекроет импорт
`google.golang.org/grpc` внутри самого пакета.

## Контракт

```go
// ingredientRepository — то, что нужно транспорту от хранилища.
// Объявляем здесь, у потребителя: серверу не нужен весь репозиторий.
type ingredientRepository interface {
	Create(ctx context.Context, in postgres.CreateIngredientParams) (int64, error)
	Get(ctx context.Context, id int64) (postgres.IngredientRow, error)
	List(ctx context.Context, limit, offset int) ([]postgres.IngredientRow, error)
}

type Server struct {
	inventoryv1.UnimplementedIngredientServiceServer
	repo ingredientRepository
}

func New(repo ingredientRepository) *Server
```

Интерфейс принимать, а не `*postgres.IngredientRepository`: `*IngredientRepository`
ему удовлетворяет автоматически, зато в тестах можно подсунуть фейк без базы.

## Маппинг ошибок

Клиент не должен видеть ошибки PostgreSQL или текст с паролем. Переводим сентинелы
в gRPC-коды:

| Что случилось | gRPC-код |
|---|---|
| `errIngredientNotFound` | `codes.NotFound` |
| `errIngredientAlreadyExists` | `codes.AlreadyExists` |
| ошибка пагинации (отрицательный `limit`/`offset`) | `codes.InvalidArgument` |
| всё остальное | `codes.Internal`, короткий текст; детали — в лог, не клиенту |

Проверять через `status.Code(err)`.

## Методы

1. **`CreateIngredient`**: собрать `postgres.CreateIngredientParams` из запроса →
   `repo.Create` → **`repo.Get` по полученному id** → вернуть `Ingredient` целиком.
   Зачем `Get`: `Create` возвращает только id, а клиенту нужны ещё времена создания
   и нормализованные поля, которые посчитала база.
2. **`GetIngredient`**: `repo.Get` → конвертер → ответ.
3. **`ListIngredients`**: `repo.List` → конвертировать каждую строку.

## Конвертеры

Отдельные функции (их удобно тестировать без сети):

```go
func rowToProto(r postgres.IngredientRow) *inventoryv1.Ingredient
```

- `description *string` → поле `optional string` (указатель остаётся указателем: `nil` → отсутствие);
- `created_at` / `updated_at` → `timestamppb.New(t)`;
- `archived_at *time.Time` → `nil` или `timestamppb.New(*t)`;
- ничего не теряем: все семь полей схемы должны доехать.

## Wiring в `cmd/main.go`

1. Читаем адрес из `INVENTORY_GRPC_ADDR` (по умолчанию `:8090`).
2. Пул → `postgres.NewIngredientRepository(pool)` → `grpcserver.New(repo)`.
3. `net.Listen("tcp", addr)`; `grpc.NewServer()`; зарегистрировать сервис.
4. Запустить `Serve` и ждать сигнал (`signal.NotifyContext`).
5. По сигналу: **`GracefulStop`** с таймаутом (несколько секунд), затем `Stop`
   для тех, кто не успел. `GracefulStop` даёт текущим запросам доиграть.
6. Закрыть пул (что открыли последним — закрываем первым).

В лог при старте: `gRPC слушает <addr>` и `application_name` из задачи 1.2.

## Тесты (сразу, не потом)

Без сети и без базы:

- конвертеры: `description` nil и не nil; `archived_at` nil и не nil; времена не нулевые;
- `CreateIngredient`: фейковый репозиторий вернул id, затем строку → ответ содержит её поля;
- `GetIngredient` с фейком, который вернул `errIngredientNotFound` → `status.Code == codes.NotFound`;
- `CreateIngredient` с `errIngredientAlreadyExists` → `codes.AlreadyExists`;
- `ListIngredients` с отрицательным `limit` → `codes.InvalidArgument`;
- `ListIngredients` с непустым результатом → все строки сконвертированы.

Фейк репозитория — три метода, ничего больше.

## Как проверю

- [ ] `make build`, `make test-race`, `make fmt-diff`, `make lint` — зелёные
- [ ] `make run` → в логе `gRPC слушает :8090`
- [ ] Ctrl+C → сервер завершается штатно, без паники и без «завис на 30 секунд»
- [ ] в коде нет утечки деталей БД в ответы клиенту (коды, а не текст ошибки)
- [ ] `Create` действительно делает `Get` после вставки и возвращает времена
- [ ] сетевые проверки (`grpcurl`) — задача 2.4

## Вопросы к размышлению (ответь в коммите)

1. Зачем объявлять `ingredientRepository` в пакете транспорта, а не импортировать
   конкретный тип репозитория?
2. Почему `UnimplementedIngredientServiceServer` встраивают, а не реализуют руками?
3. Почему клиенту нельзя отдавать ошибку БД как есть?
4. Чем `GracefulStop` лучше `Stop` и зачем после него ещё и таймаут?
5. Почему `CreateIngredient` возвращает ингредиент, а не только id?
