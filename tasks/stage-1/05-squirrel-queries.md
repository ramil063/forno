# Задача 1.5 — запросы через squirrel: вставка с возвратом ID, nullable-поля

- **Этап:** 1, «PostgreSQL с самого начала: инфраструктура и хранилище»
- **Оценка:** ~2 часа
- **Зависимости:** схема (задача 1.4) и пул (1.2) — готовы
- **Артефакт:** `services/inventory-service/internal/storage/postgres/ingredient.go`
  (+ тесты)
- **Готово, когда:** SQL для создания и чтения ингредиента собирается squirrel,
  без склейки строк, и это проверено тестами без базы.

## Файлы

```
services/inventory-service/internal/storage/postgres/
  ingredient.go        запросы к таблице ingredients
  ingredient_test.go   тесты сборки SQL, база не нужна
  errors.go            + ошибки уровня storage
```

## Контракты (форма; имена можно менять)

```go
// IngredientRow — строка таблицы ingredients в том виде, как она лежит в базе.
type IngredientRow struct {
	ID          int64
	Name        string
	UnitID      int64
	Description *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ArchivedAt  *time.Time
}

func CreateIngredient(ctx context.Context, db DBTX, in CreateIngredientParams) (int64, error)
func GetIngredient(ctx context.Context, db DBTX, id int64) (IngredientRow, error)
func ListIngredients(ctx context.Context, db DBTX, limit, offset int) ([]IngredientRow, error)
```

`DBTX` — маленький интерфейс, который объявляет **этот** пакет (он потребитель базы):

```go
type DBTX interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}
```

`*pgxpool.Pool` и `pgx.Tx` ему удовлетворяют. Зачем интерфейс, а не `*pgxpool.Pool`:
на этапе 6 внутрь без правок войдёт транзакция, а в тестах можно подсунуть фейк.

## Требования

1. **Только squirrel.** `squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)`.
   Значения — через `Values`/`Where`/`SetMap`, имена таблиц и колонок — константами в коде.
   Никакой конкатенации строк с пользовательскими данными: конкатенация — это SQL-инъекция,
   а `$1`-плейсхолдеры отделяют код от данных.
2. **Вставка с возвратом ID:** `INSERT ... RETURNING id`, затем `QueryRow(...).Scan(&id)`.
   `LastInsertId` в PostgreSQL нет — `RETURNING` штатный способ.
3. **`name_normalized` не вставляем** — это generated column, база считает её сама.
   Передаём только `name`.
4. **Nullable-поля:** `description` и `archived_at` — указатели (`*string`, `*time.Time`).
   `nil` при вставке должен стать `NULL`, `NULL` при чтении — `nil`. `name` и `unit_id`
   обязательны.
5. **`created_at` / `updated_at` не передаём** — их ставит база (`DEFAULT`, триггер).
6. **Ошибки уровня storage:**
   - `pgx.ErrNoRows` → `ErrIngredientNotFound`;
   - нарушение уникальности (PostgreSQL SQLSTATE `23505`) → `ErrIngredientAlreadyExists`.
   Разворачивать через `errors.Is`/`errors.As`, причину сохранять через `%w`. Слой выше
   не должен знать, что база — PostgreSQL.
7. **Список:** только неархивные (`archived_at IS NULL`), сортировка по `name`,
   `LIMIT`/`OFFSET`. Поиск по имени в этой задаче не делаем.
8. **Никаких `SELECT *`** — колонки перечисляем явно. Иначе добавление колонки в схему
   сломает `Scan` на ровном месте.

## Тесты без базы

Squirrel умеет показать готовый SQL:

```go
sql, args, err := query.ToSql()
```

Чтобы это было удобно тестировать, вынеси сборку в маленькие функции:

```go
func insertIngredientQuery(in CreateIngredientParams) squirrel.InsertBuilder
func getIngredientQuery(id int64) squirrel.SelectBuilder
func listIngredientsQuery(limit, offset int) squirrel.SelectBuilder
```

Тогда основной код вызывает их и выполняет, а тесты проверяют сборку:

- в SQL есть `$1`, `$2`, ..., а значения лежат в `args`, а не в тексте (доказательство,
  что нет склейки);
- `INSERT` оканчивается `RETURNING id`;
- `SELECT` перечисляет колонки, а не `*`;
- `List` содержит `archived_at IS NULL`, `ORDER BY` и `LIMIT`;
- nullable-значение попадает в `args` как `nil` — pgx отправит `NULL`.

Реальное сканирование `NULL`, срабатывание уникальности и `ErrNoRows` проверяются
на живой базе в задаче 7.

## Как проверю

- [ ] `make fmt-diff`, `make lint`, `make test-race` — зелёные
- [ ] тесты сборки SQL проверяют плейсхолдеры и аргументы, а не текст с подставленными
      значениями
- [ ] в коде нет конкатенации SQL с пользовательскими данными (прочитаю глазами)
- [ ] ошибки обёрнуты: `errors.Is(err, ErrIngredientNotFound)` работает после
      `Scan`/`Row`-цепочки
- [ ] `List` не отдаёт архивные ингредиенты и не использует `SELECT *`

## Вопросы к размышлению (ответь в коммите)

1. Почему `$1` и args лучше, чем `fmt.Sprintf`? Что конкретно может сделать пользователь
   со строкой имени, если её вклеить в SQL?
2. Почему `RETURNING id` вместо `LastInsertId`?
3. Чем `*string` отличается от `sql.NullString`? Что выберешь для `description` и почему?
4. Почему `SELECT *` опасен при `Scan`?
5. Зачем превращать SQLSTATE `23505` в `ErrIngredientAlreadyExists`, если можно отдать
   ошибку как есть?
6. Зачем интерфейс `DBTX`, если можно принять `*pgxpool.Pool`? Подсказка: этап 6.
