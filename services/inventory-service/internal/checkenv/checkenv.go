package checkenv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/puddle/v2"
	"golang.org/x/sync/errgroup"

	"github.com/ramil063/forno/services/inventory-service/internal/storage/postgres"
)

// Check — одна проверка окружения с именем для отчёта.
type Check struct {
	Name string
	Run  func(ctx context.Context, pool *pgxpool.Pool) (detail string, err error)
}

// Result — итог одной проверки.
type Result struct {
	Name    string
	Detail  string
	Err     error
	Skipped bool
}

var checks = []Check{
	{"ping", checkPing},
	{"db", checkDB},
	{"pool", checkPoolConnection},
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Run создаёт пул, выполняет проверки параллельно и возвращает итог по каждой.
func Run(ctx context.Context, cfg postgres.Config) ([]Result, error) {
	pool, err := postgres.New(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errCreatePool, err)
	}
	defer pool.Close()

	return runChecks(ctx, pool, checks)
}

func runChecks(ctx context.Context, pool *pgxpool.Pool, list []Check) ([]Result, error) {

	errGroup, errGroupCtx := errgroup.WithContext(ctx)
	results := make([]Result, len(list))

	for i, check := range list {
		i, check := i, check
		errGroup.Go(func() error {
			detail, runErr := check.Run(errGroupCtx, pool)
			results[i] = Result{Name: check.Name, Detail: detail, Err: runErr}
			return runErr
		})
	}
	_ = errGroup.Wait()

	groupCanceled := errors.Is(errGroupCtx.Err(), context.Canceled)
	for i := range results {
		if results[i].Err != nil && groupCanceled && errors.Is(results[i].Err, context.Canceled) {
			results[i].Skipped = true
		}
	}

	var joined []error
	for _, r := range results {
		if r.Err != nil && !r.Skipped {
			joined = append(joined, fmt.Errorf("%s: %w", r.Name, r.Err))
		}
	}

	return results, errors.Join(joined...)
}

func checkPing(ctx context.Context, pool *pgxpool.Pool) (detail string, err error) {
	if err := pool.Ping(ctx); err != nil {
		return "", fmt.Errorf("%w: %w", errPingDB, err)
	}
	return "", nil
}

func checkDB(ctx context.Context, pool *pgxpool.Pool) (detail string, err error) {
	if err = checkTables(ctx, pool); err != nil {
		return "", err
	}
	if err = checkMigrations(ctx, pool); err != nil {
		return "", err
	}
	return "", nil
}

func checkTables(ctx context.Context, pool querier) error {
	var requiredTables = []string{
		"ingredients",
		"units",
		"warehouses",
		"ingredient_stock",
	}
	const query = `SELECT to_regclass($1) IS NOT NULL`

	for _, table := range requiredTables {
		var exists bool
		if err := pool.QueryRow(ctx, query, "public."+table).Scan(&exists); err != nil {
			return fmt.Errorf("проверка таблицы %s: %w", table, err)
		}
		if !exists {
			return fmt.Errorf("нет таблицы %s", table)
		}
	}
	return nil
}

func checkMigrations(ctx context.Context, pool querier) error {
	const query = `
		SELECT version_id, is_applied
		FROM goose_db_version
		ORDER BY id DESC
		LIMIT 1`

	var (
		versionID int64
		isApplied bool
	)
	if err := pool.QueryRow(ctx, query).Scan(&versionID, &isApplied); err != nil {
		return fmt.Errorf("чтение goose_db_version: %w", err)
	}

	if !isApplied {
		return fmt.Errorf("последняя миграция не применена: version_id=%d, is_applied=false", versionID)
	}
	if versionID == 0 {
		return fmt.Errorf("последняя применённая версия — 0 (база пустая, миграции не накатывались)")
	}
	return nil
}

func checkPoolConnection(ctx context.Context, pool *pgxpool.Pool) (detail string, err error) {
	acquireTimeout := 2 * time.Second

	ctxAcquire, cancel := context.WithTimeout(ctx, acquireTimeout)
	defer cancel()

	conn, err := pool.Acquire(ctxAcquire)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return "", fmt.Errorf("не дождались свободного соединения за %s: %w", acquireTimeout, err)
		case errors.Is(err, context.Canceled):
			return "", fmt.Errorf("получение соединения отменено: %w", err)
		case errors.Is(err, puddle.ErrClosedPool):
			return "", fmt.Errorf("пул закрыт: %w", err)
		default:
			return "", fmt.Errorf("не удалось получить соединение из пула: %w", err)
		}
	}
	defer conn.Release()

	var appName, appSetTimeout string
	err = conn.QueryRow(
		ctxAcquire,
		"SELECT current_setting('application_name'),"+
			"		current_setting('statement_timeout')").
		Scan(&appName, &appSetTimeout)
	if err != nil {
		return "", fmt.Errorf("чтение настроек сессии: %w", err)
	}
	if appName == "" || appSetTimeout == "" {
		return "", errEmptyAppName
	}

	stat := pool.Stat()
	detail = fmt.Sprintf(
		"pool: max=%d total=%d idle=%d acquired=%d empty_acquire=%d canceled_acquire=%d",
		stat.MaxConns(),
		stat.TotalConns(),
		stat.IdleConns(),
		stat.AcquiredConns(),
		stat.EmptyAcquireCount(),
		stat.CanceledAcquireCount(),
	)
	return detail, nil
}
