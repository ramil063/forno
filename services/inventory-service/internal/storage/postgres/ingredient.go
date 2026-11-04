package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

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

// CreateIngredientParams - структура для создания ингредиента
type CreateIngredientParams struct {
	Name        string
	UnitID      int64
	Description *string
}

// DBTX - интерфейс для обращения к БД
type DBTX interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// psql — общий StatementBuilder с $1,$2,... для всех запросов пакета.
// Один раз настроили — и не забываем PlaceholderFormat в каждом билдере.
var psql = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

const (
	defaultLimit = 50
	maxLimit     = 200
)

// ---------------------------------------------------------------------------
// Билдеры запросов. Отдельные функции — чтобы их было удобно тестировать.
// ---------------------------------------------------------------------------

func insertIngredientQuery(in CreateIngredientParams) sq.InsertBuilder {
	return psql.
		Insert("ingredients").
		Columns("name", "unit_id", "description").
		Values(in.Name, in.UnitID, in.Description).
		Suffix("RETURNING id")
}

func getIngredientQuery(id int64) sq.SelectBuilder {
	return psql.
		Select("id", "name", "unit_id", "description", "created_at", "updated_at", "archived_at").
		From("ingredients").
		Where(sq.Eq{"id": id})
}

func listIngredientsQuery(limit, offset int) sq.SelectBuilder {
	return psql.
		Select("id", "name", "unit_id", "description", "created_at", "updated_at", "archived_at").
		From("ingredients").
		Where(sq.Eq{"archived_at": nil}). // → archived_at IS NULL
		OrderBy("name").
		Limit(uint64(limit)).
		Offset(uint64(offset))
}

func normalizePagination(limit, offset int) (int, int, error) {
	if limit < 0 || offset < 0 {
		return 0, 0, fmt.Errorf("limit и offset не могут быть отрицательными: limit=%d offset=%d", limit, offset)
	}
	if limit == 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return limit, offset, nil
}

// ---------------------------------------------------------------------------
// Основные функции — только сборка + выполнение.
// ---------------------------------------------------------------------------

// CreateIngredient - создание ингредиента
func CreateIngredient(ctx context.Context, db DBTX, in CreateIngredientParams) (int64, error) {
	query, args, err := insertIngredientQuery(in).ToSql()
	if err != nil {
		return 0, fmt.Errorf("построение запроса: %w", err)
	}

	var id int64
	if err = db.QueryRow(ctx, query, args...).Scan(&id); err != nil {
		return 0, mapInsertIngredientError(err)
	}
	return id, nil
}

// GetIngredient - получение ингредиента
func GetIngredient(ctx context.Context, db DBTX, id int64) (IngredientRow, error) {
	query, args, err := getIngredientQuery(id).ToSql()
	if err != nil {
		return IngredientRow{}, fmt.Errorf("построение запроса: %w", err)
	}

	var row IngredientRow
	err = db.QueryRow(ctx, query, args...).
		Scan(&row.ID, &row.Name, &row.UnitID, &row.Description,
			&row.CreatedAt, &row.UpdatedAt, &row.ArchivedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IngredientRow{}, fmt.Errorf("%w: id=%d", errIngredientNotFound, id)
		}
		return IngredientRow{}, fmt.Errorf("чтение ингредиента: %w", err)
	}
	return row, nil
}

// ListIngredients - список ингредиентов
func ListIngredients(ctx context.Context, db DBTX, limit, offset int) ([]IngredientRow, error) {
	limit, offset, err := normalizePagination(limit, offset)
	if err != nil {
		return nil, err
	}

	query, args, err := listIngredientsQuery(limit, offset).ToSql()
	if err != nil {
		return nil, fmt.Errorf("построение запроса: %w", err)
	}

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("запрос списка ингредиентов: %w", err)
	}
	defer rows.Close()

	out := make([]IngredientRow, 0, limit)
	for rows.Next() {
		var r IngredientRow
		if err = rows.Scan(&r.ID, &r.Name, &r.UnitID, &r.Description,
			&r.CreatedAt, &r.UpdatedAt, &r.ArchivedAt); err != nil {
			return nil, fmt.Errorf("чтение строки ингредиента: %w", err)
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("итерация по строкам: %w", err)
	}
	return out, nil
}

func mapInsertIngredientError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: %w", errIngredientAlreadyExists, err)
	}
	return fmt.Errorf("вставка ингредиента: %w", err)
}
