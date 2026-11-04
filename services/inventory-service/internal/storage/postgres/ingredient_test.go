package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// ---------------------------------------------------------------------------
// insertIngredientQuery
// ---------------------------------------------------------------------------

func TestInsertIngredientQuery(t *testing.T) {
	desc := "спелый"
	sql, args, err := insertIngredientQuery(CreateIngredientParams{
		Name:        "Томат",
		UnitID:      1,
		Description: &desc,
	}).ToSql()
	if err != nil {
		t.Fatalf("ToSql: %v", err)
	}

	// 1. Плейсхолдеры $1..$3 — доказательство, что нет склейки.
	for _, p := range []string{"$1", "$2", "$3"} {
		if !strings.Contains(sql, p) {
			t.Errorf("ожидали %s в SQL, получили: %s", p, sql)
		}
	}

	// 2. Значения НЕ в тексте SQL — они в args.
	for _, v := range []string{"Томат", "спелый"} {
		if strings.Contains(sql, v) {
			t.Errorf("значение %q не должно быть в тексте SQL: %s", v, sql)
		}
	}

	// 3. INSERT оканчивается RETURNING id.
	if !strings.HasSuffix(strings.TrimSpace(sql), "RETURNING id") {
		t.Errorf("ожидали RETURNING id в конце, получили: %s", sql)
	}

	// 4. args — ровно то, что мы передали, в том же порядке.
	wantArgs := []any{"Томат", int64(1), &desc}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args:\n  want %+v\n  got  %+v", wantArgs, args)
	}
}

func TestInsertIngredientQuery_NilDescription(t *testing.T) {
	sql, args, err := insertIngredientQuery(CreateIngredientParams{
		Name:        "Томат",
		UnitID:      1,
		Description: nil,
	}).ToSql()
	if err != nil {
		t.Fatalf("ToSql: %v", err)
	}
	_ = sql

	if len(args) != 3 {
		t.Fatalf("ожидали 3 args, получили %d: %+v", len(args), args)
	}
	// Третий аргумент должен быть typed nil (*string)(nil) — pgx отправит NULL.
	d, ok := args[2].(*string)
	if !ok {
		t.Fatalf("третий аргумент должен быть *string, получили %T", args[2])
	}
	if d != nil {
		t.Errorf("ожидали (*string)(nil), получили указатель на %q", *d)
	}
}

// ---------------------------------------------------------------------------
// getIngredientQuery
// ---------------------------------------------------------------------------

func TestGetIngredientQuery(t *testing.T) {
	sql, args, err := getIngredientQuery(42).ToSql()
	if err != nil {
		t.Fatalf("ToSql: %v", err)
	}

	// 1. Это SELECT, и он перечисляет колонки, а не *.
	if !strings.HasPrefix(sql, "SELECT ") {
		t.Errorf("ожидали SELECT в начале, получили: %s", sql)
	}
	if strings.Contains(sql, "*") {
		t.Errorf("SELECT не должен содержать *, получили: %s", sql)
	}

	// 2. WHERE по id через плейсхолдер.
	if !strings.Contains(sql, "WHERE id = $1") {
		t.Errorf("ожидали 'WHERE id = $1', получили: %s", sql)
	}

	// 3. Значение id — в args, а не в тексте.
	if strings.Contains(sql, "42") {
		t.Errorf("id не должен быть в тексте SQL: %s", sql)
	}
	wantArgs := []any{int64(42)}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args: want %+v, got %+v", wantArgs, args)
	}
	selectPart, _, ok := strings.Cut(sql, " FROM ")
	if !ok {
		t.Fatalf("не нашли FROM в SQL: %s", sql)
	}
	for _, col := range []string{"created_at", "updated_at", "archived_at"} {
		if !strings.Contains(selectPart, col) {
			t.Errorf("колонка %s должна быть в SELECT, получили: %s", col, selectPart)
		}
	}
}

// ---------------------------------------------------------------------------
// listIngredientsQuery
// ---------------------------------------------------------------------------

func TestListIngredientsQuery(t *testing.T) {
	sql, args, err := listIngredientsQuery(10, 20).ToSql()
	if err != nil {
		t.Fatalf("ToSql: %v", err)
	}

	// 1. SELECT перечисляет колонки, а не *.
	if !strings.HasPrefix(sql, "SELECT ") {
		t.Errorf("ожидали SELECT в начале, получили: %s", sql)
	}
	if strings.Contains(sql, "*") {
		t.Errorf("SELECT не должен содержать *, получили: %s", sql)
	}

	// 2. Архивные отфильтрованы.
	if !strings.Contains(sql, "archived_at IS NULL") {
		t.Errorf("ожидали 'archived_at IS NULL', получили: %s", sql)
	}

	// 3. Порядок детерминирован — иначе LIMIT/OFFSET будет плавать.
	if !strings.Contains(sql, "ORDER BY name") {
		t.Errorf("ожидали 'ORDER BY name', получили: %s", sql)
	}

	// 4. LIMIT/OFFSET вшиты как числа (не плейсхолдеры) — так работает Squirrel.
	if !strings.Contains(sql, "LIMIT 10") {
		t.Errorf("ожидали 'LIMIT 10', получили: %s", sql)
	}
	if !strings.Contains(sql, "OFFSET 20") {
		t.Errorf("ожидали 'OFFSET 20', получили: %s", sql)
	}

	// 5. У этого запроса нет параметров — все значения вшиты безопасно.
	if len(args) != 0 {
		t.Errorf("ожидали 0 args, получили %+v", args)
	}
	selectPart, _, ok := strings.Cut(sql, " FROM ")
	if !ok {
		t.Fatalf("не нашли FROM в SQL: %s", sql)
	}
	for _, col := range []string{"created_at", "updated_at", "archived_at"} {
		if !strings.Contains(selectPart, col) {
			t.Errorf("колонка %s должна быть в SELECT, получили: %s", col, selectPart)
		}
	}
}

func TestMapInsertIngredientError(t *testing.T) {
	t.Run("unique violation", func(t *testing.T) {
		err := mapInsertIngredientError(&pgconn.PgError{Code: "23505"})

		if !errors.Is(err, errIngredientAlreadyExists) {
			t.Errorf("ожидали errIngredientAlreadyExists, получили %v", err)
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			t.Errorf("исходная ошибка должна сохраняться: %v", err)
		}
	})

	t.Run("other error", func(t *testing.T) {
		orig := errors.New("connection refused")
		err := mapInsertIngredientError(orig)

		if errors.Is(err, errIngredientAlreadyExists) {
			t.Errorf("чужую ошибку не маппим в already exists: %v", err)
		}
		if !errors.Is(err, orig) {
			t.Errorf("исходная ошибка должна сохраняться: %v", err)
		}
	})
}

func TestListIngredients_NegativeLimit(t *testing.T) {
	_, err := ListIngredients(context.Background(), nil, -1, 0)
	if err == nil {
		t.Fatal("ожидали ошибку на отрицательном limit")
	}
}

func TestNormalizePagination(t *testing.T) {
	tests := []struct {
		name       string
		limit      int
		offset     int
		wantLimit  int
		wantOffset int
		wantErr    bool
	}{
		{"отрицательный limit", -1, 0, 0, 0, true},
		{"отрицательный offset", 0, -1, 0, 0, true},
		{"limit по умолчанию", 0, 0, defaultLimit, 0, false},
		{"значения как есть", 10, 20, 10, 20, false},
		{"limit выше максимума", 1000, 0, maxLimit, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLimit, gotOffset, err := normalizePagination(tt.limit, tt.offset)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ошибка: ожидали wantErr=%v, получили %v", tt.wantErr, err)
			}
			if err != nil {
				return
			}
			if gotLimit != tt.wantLimit || gotOffset != tt.wantOffset {
				t.Errorf("получили limit=%d offset=%d, ожидали limit=%d offset=%d",
					gotLimit, gotOffset, tt.wantLimit, tt.wantOffset)
			}
		})
	}
}
