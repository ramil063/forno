package checkenv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ramil063/forno/services/inventory-service/internal/storage/postgres"
)

// ---------------------------------------------------------------------------
// Структурные тесты — проверяют метаданные checks, работают без БД.
// ---------------------------------------------------------------------------

func TestChecks_NotEmpty(t *testing.T) {
	if len(checks) == 0 {
		t.Fatal("checks пуст — нечего запускать")
	}
}

func TestChecks_NoDuplicateNames(t *testing.T) {
	seen := make(map[string]struct{}, len(checks))
	for _, c := range checks {
		if c.Name == "" {
			t.Errorf("проверка с пустым именем: %+v", c)
		}
		if c.Run == nil {
			t.Errorf("проверка %q без Run", c.Name)
		}
		if _, dup := seen[c.Name]; dup {
			t.Errorf("дубликат имени проверки: %q", c.Name)
		}
		seen[c.Name] = struct{}{}
	}
}

// ---------------------------------------------------------------------------
// Run — тесты на раннем выходе при плохой конфигурации. До БД дело не доходит,
// postgres.New падает на валидации DSN.
// ---------------------------------------------------------------------------

func TestRun_EmptyDSN(t *testing.T) {
	results, err := Run(context.Background(), postgres.Config{DSN: ""})
	if err == nil {
		t.Fatal("ожидали ошибку на пустом DSN, получили nil")
	}
	if results != nil {
		t.Errorf("results должны быть nil при ошибке создания пула, получили %v", results)
	}
	if !errors.Is(err, errCreatePool) {
		t.Errorf("ожидали errCreatePool, получили %v", err)
	}
}

func TestRun_WhitespaceDSN(t *testing.T) {
	results, err := Run(context.Background(), postgres.Config{DSN: "   "})
	if err == nil {
		t.Fatal("ожидали ошибку на DSN из пробелов, получили nil")
	}
	if results != nil {
		t.Errorf("results должны быть nil, получили %v", results)
	}
}

func TestRun_InvalidDSN(t *testing.T) {
	results, err := Run(context.Background(), postgres.Config{DSN: "://broken"})
	if err == nil {
		t.Fatal("ожидали ошибку на невалидном DSN, получили nil")
	}
	if results != nil {
		t.Errorf("results должны быть nil, получили %v", results)
	}
}

// ---------------------------------------------------------------------------
// Фейки для querier/pgx.Row.
// ---------------------------------------------------------------------------

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan: ожидалось %d значений, получено %d", len(dest), len(r.values))
	}
	for i, v := range r.values {
		switch d := dest[i].(type) {
		case *bool:
			b, ok := v.(bool)
			if !ok {
				return fmt.Errorf("scan: нельзя %T в *bool", v)
			}
			*d = b
		case *int64:
			n, ok := v.(int64)
			if !ok {
				return fmt.Errorf("scan: нельзя %T в *int64", v)
			}
			*d = n
		case *string:
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("scan: нельзя %T в *string", v)
			}
			*d = s
		default:
			return fmt.Errorf("scan: неподдерживаемый тип %T", dest[i])
		}
	}
	return nil
}

// fakeQuerier отвечает на два вида SQL, которые шлют checkTables/checkMigrations.
type fakeQuerier struct {
	tables  map[string]bool // "public.ingredients" -> exists
	migrVer int64
	migrApp bool
	migrErr error // если задан — QueryRow вернёт эту ошибку (эмуляция ErrNoRows и т.п.)
}

func (q fakeQuerier) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "to_regclass"):
		if len(args) != 1 {
			return fakeRow{err: fmt.Errorf("to_regclass: ожидали 1 аргумент, получили %d", len(args))}
		}
		name, _ := args[0].(string)
		exists, ok := q.tables[name]
		if !ok {
			return fakeRow{err: fmt.Errorf("неожиданная таблица в тесте: %s", name)}
		}
		return fakeRow{values: []any{exists}}
	case strings.Contains(sql, "goose_db_version"):
		if q.migrErr != nil {
			return fakeRow{err: q.migrErr}
		}
		return fakeRow{values: []any{q.migrVer, q.migrApp}}
	default:
		return fakeRow{err: fmt.Errorf("неожиданный sql: %s", sql)}
	}
}

// ---------------------------------------------------------------------------
// checkTables
// ---------------------------------------------------------------------------

func TestCheckTables_AllPresent(t *testing.T) {
	q := fakeQuerier{tables: map[string]bool{
		"public.ingredients":      true,
		"public.units":            true,
		"public.warehouses":       true,
		"public.ingredient_stock": true,
	}}
	if err := checkTables(context.Background(), q); err != nil {
		t.Fatalf("ожидали nil, получили %v", err)
	}
}

func TestCheckTables_MissingTable(t *testing.T) {
	q := fakeQuerier{tables: map[string]bool{
		"public.ingredients":      true,
		"public.units":            true,
		"public.warehouses":       true,
		"public.ingredient_stock": false, // ← вот она
	}}
	err := checkTables(context.Background(), q)
	if err == nil {
		t.Fatal("ожидали ошибку, получили nil")
	}
	if !strings.Contains(err.Error(), "ingredient_stock") {
		t.Errorf("в тексте ошибки должно быть имя таблицы, получили: %v", err)
	}
}

func TestCheckTables_QueryError(t *testing.T) {
	// Ошибка драйвера на первом же запросе должна обернуться с именем таблицы.
	q := fakeQuerier{tables: map[string]bool{}} // пустой map → "неожиданная таблица" внутри fakeQuerier
	err := checkTables(context.Background(), q)
	if err == nil {
		t.Fatal("ожидали ошибку, получили nil")
	}
	if !strings.Contains(err.Error(), "проверка таблицы") {
		t.Errorf("ожидали обёртку 'проверка таблицы', получили: %v", err)
	}
}

// ---------------------------------------------------------------------------
// checkMigrations
// ---------------------------------------------------------------------------

func TestCheckMigrations_OK(t *testing.T) {
	q := fakeQuerier{migrVer: 20240101120000, migrApp: true}
	if err := checkMigrations(context.Background(), q); err != nil {
		t.Fatalf("ожидали nil, получили %v", err)
	}
}

func TestCheckMigrations_NotApplied(t *testing.T) {
	q := fakeQuerier{migrVer: 20240101120000, migrApp: false}
	err := checkMigrations(context.Background(), q)
	if err == nil {
		t.Fatal("ожидали ошибку, получили nil")
	}
	if !strings.Contains(err.Error(), "не применена") {
		t.Errorf("ожидали текст про 'не применена', получили: %v", err)
	}
	if !strings.Contains(err.Error(), "is_applied=false") {
		t.Errorf("ожидали is_applied=false в тексте, получили: %v", err)
	}
}

func TestCheckMigrations_VersionZero(t *testing.T) {
	q := fakeQuerier{migrVer: 0, migrApp: true}
	err := checkMigrations(context.Background(), q)
	if err == nil {
		t.Fatal("ожидали ошибку, получили nil")
	}
	if !strings.Contains(err.Error(), "версия — 0") {
		t.Errorf("ожидали текст про версию 0, получили: %v", err)
	}
}

func TestCheckMigrations_TableMissing(t *testing.T) {
	// Эмулируем ситуацию, когда goose_db_version вообще нет —
	// QueryRow(...).Scan(...) вернёт pgx.ErrNoRows.
	q := fakeQuerier{migrErr: pgx.ErrNoRows}
	err := checkMigrations(context.Background(), q)
	if err == nil {
		t.Fatal("ожидали ошибку, получили nil")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("ожидали обёрнутый pgx.ErrNoRows, получили: %v", err)
	}
	if !strings.Contains(err.Error(), "goose_db_version") {
		t.Errorf("ожидали имя таблицы в тексте ошибки, получили: %v", err)
	}
}

// ---------------------------------------------------------------------------
// checkDB — собирает таблицы + миграции, проверяем что обе ветки доходят.
// ---------------------------------------------------------------------------

func TestCheckDB_AllGood(t *testing.T) {
	q := fakeQuerier{
		tables: map[string]bool{
			"public.ingredients":      true,
			"public.units":            true,
			"public.warehouses":       true,
			"public.ingredient_stock": true,
		},
		migrVer: 20240101120000,
		migrApp: true,
	}
	if err := checkTables(context.Background(), q); err != nil {
		t.Fatalf("checkTables: %v", err)
	}
	if err := checkMigrations(context.Background(), q); err != nil {
		t.Fatalf("checkMigrations: %v", err)
	}
}

func TestCheckDB_TablesFailShortCircuits(t *testing.T) {
	// Если таблицы отсутствуют — миграции не проверяются.
	// fakeQuerier с пустым tables упадёт на первой таблице.
	q := fakeQuerier{}
	if err := checkTables(context.Background(), q); err == nil {
		t.Fatal("ожидали ошибку на отсутствующих таблицах")
	}
}

// ---------------------------------------------------------------------------
// runChecks — оркестрация: параллельный запуск, сбор ошибок, ok/fail/skip.
//
// Фейковые проверки пул не используют, поэтому передаём nil: база не нужна.
// ---------------------------------------------------------------------------

func fakeCheck(name, detail string, err error) Check {
	return Check{
		Name: name,
		Run: func(_ context.Context, _ *pgxpool.Pool) (string, error) {
			return detail, err
		},
	}
}

// blockingCheck висит, пока не отменят контекст, и возвращает причину отмены.
func blockingCheck(name string) Check {
	return Check{
		Name: name,
		Run: func(ctx context.Context, _ *pgxpool.Pool) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
}

func resultByName(t *testing.T, results []Result, name string) Result {
	t.Helper()
	for _, r := range results {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("нет результата проверки %q среди %+v", name, results)
	return Result{}
}

func TestRunChecks_AllOK(t *testing.T) {
	list := []Check{
		fakeCheck("one", "полезная деталь", nil),
		fakeCheck("two", "", nil),
	}

	results, err := runChecks(context.Background(), nil, list)
	if err != nil {
		t.Fatalf("ожидали nil, получили %v", err)
	}
	if len(results) != len(list) {
		t.Fatalf("ожидали %d результатов, получили %d", len(list), len(results))
	}

	// Результаты должны идти в порядке списка проверок, несмотря на параллельность.
	if results[0].Name != "one" || results[1].Name != "two" {
		t.Errorf("порядок результатов не совпал со списком: %+v", results)
	}
	for _, r := range results {
		if r.Err != nil || r.Skipped {
			t.Errorf("%s: ожидали ok, получили err=%v skipped=%v", r.Name, r.Err, r.Skipped)
		}
	}
	if got := resultByName(t, results, "one").Detail; got != "полезная деталь" {
		t.Errorf("detail не доехал до результата: %q", got)
	}
}

func TestRunChecks_OneFails(t *testing.T) {
	boom := errors.New("сломалось")
	list := []Check{
		fakeCheck("ok1", "", nil),
		fakeCheck("bad", "", boom),
		fakeCheck("ok2", "", nil),
	}

	results, err := runChecks(context.Background(), nil, list)
	if err == nil {
		t.Fatal("ожидали ошибку, получили nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("итоговая ошибка должна сохранять причину: %v", err)
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Errorf("в итоговой ошибке должно быть имя упавшей проверки: %v", err)
	}
	if r := resultByName(t, results, "bad"); r.Skipped {
		t.Error("настоящая ошибка не должна помечаться как skip")
	}
	for _, name := range []string{"ok1", "ok2"} {
		if r := resultByName(t, results, name); r.Err != nil {
			t.Errorf("%s: ожидали ok, получили %v", name, r.Err)
		}
	}
}

func TestRunChecks_AllFail(t *testing.T) {
	errA := errors.New("ошибка A")
	errB := errors.New("ошибка B")
	list := []Check{
		fakeCheck("a", "", errA),
		fakeCheck("b", "", errB),
	}

	results, err := runChecks(context.Background(), nil, list)
	if err == nil {
		t.Fatal("ожидали ошибку, получили nil")
	}
	if err.Error() == "" {
		t.Fatalf("итоговая ошибка пустая: %v", err)
	}
	for _, want := range []string{"a", "b"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в итоговой ошибке должно быть имя %q: %v", want, err)
		}
	}
	if !errors.Is(err, errA) || !errors.Is(err, errB) {
		t.Errorf("errors.Join должен сохранять обе причины: %v", err)
	}
	for _, r := range results {
		if r.Skipped {
			t.Errorf("%s: настоящая ошибка не должна быть skip", r.Name)
		}
	}
}

func TestRunChecks_DeadlineExpired_IsFailNotSkip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	results, err := runChecks(ctx, nil, []Check{blockingCheck("slow")})
	if err == nil {
		t.Fatal("ожидали ошибку по таймауту, получили nil")
	}
	r := resultByName(t, results, "slow")
	if r.Err == nil {
		t.Fatal("ожидали ошибку у проверки slow")
	}
	if r.Skipped {
		t.Errorf("истёкший дедлайн — это fail, а не skip: %+v", r)
	}
}

func TestRunChecks_CanceledBySibling_IsSkip(t *testing.T) {
	boom := errors.New("сломалось")
	list := []Check{
		fakeCheck("bad", "", boom), // падает сразу и отменяет соседа
		blockingCheck("waiting"),   // висит до отмены и вернёт context.Canceled
	}

	results, err := runChecks(context.Background(), nil, list)
	if err == nil {
		t.Fatal("ожидали ошибку, получили nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("итоговая ошибка должна сохранять причину: %v", err)
	}

	r := resultByName(t, results, "waiting")
	if !r.Skipped {
		t.Errorf("отменённая соседом проверка должна быть skip, получили: %+v", r)
	}
	if strings.Contains(err.Error(), "waiting") {
		t.Errorf("skip не должен попадать в итоговую ошибку: %v", err)
	}
}
