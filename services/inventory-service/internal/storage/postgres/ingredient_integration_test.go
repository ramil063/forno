//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// newTestRepo даёт repo внутри транзакции с уже очищенной таблицей ingredients.
// Rollback в cleanup откатит всё — БД остаётся как была.
func newTestRepo(t *testing.T) (context.Context, *IngredientRepository) {
	t.Helper()

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("POSTGRES_DSN не задан: тест запускается внутри контейнера (make test-integration)")
	}

	ctx := context.Background()

	pool, err := New(ctx, Config{DSN: dsn, ApplicationName: "inventory-service-test"})
	if err != nil {
		t.Fatalf("пул: %v", err)
	}
	t.Cleanup(pool.Close)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("транзакция: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	// Чистим таблицу, чтобы не зависеть от seed-данных из миграций.
	// CASCADE — на случай FK из ingredient_stock. Rollback вернёт всё как было.
	if _, err = tx.Exec(ctx, "TRUNCATE ingredients RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("очистка ingredients: %v", err)
	}

	return ctx, NewIngredientRepository(tx)
}

// seedUnit берёт id первого unit'а или создаёт тестовый.
// Нужен, если ingredients.unit_id — FK на units.
func seedUnit(ctx context.Context, t *testing.T, repo *IngredientRepository) int64 {
	t.Helper()
	var id int64
	err := repo.db.QueryRow(ctx, `SELECT id FROM units ORDER BY id LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("в units нет ни одной единицы: накати миграции (make migrate-up)")
	}
	if err != nil {
		t.Fatalf("seed unit: %v", err)
	}
	return id
}

func mustCreate(ctx context.Context, t *testing.T, repo *IngredientRepository, in CreateIngredientParams) int64 {
	t.Helper()
	id, err := repo.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create(%+v): %v", in, err)
	}
	if id <= 0 {
		t.Fatalf("Create(%+v): id=%d, want > 0", in, id)
	}
	return id
}

// ---------------------------------------------------------------------------
// 1. Круговой рейс: Create → Get отдаёт то же самое.
// ---------------------------------------------------------------------------

func TestCreateGet_RoundTrip(t *testing.T) {
	ctx, repo := newTestRepo(t)
	unitID := seedUnit(ctx, t, repo)

	desc := "спелый"
	id := mustCreate(ctx, t, repo, CreateIngredientParams{
		Name: "Томат", UnitID: unitID, Description: &desc,
	})

	got, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get(%d): %v", id, err)
	}

	if got.ID != id {
		t.Errorf("ID: want %d, got %d", id, got.ID)
	}
	if got.Name != "Томат" {
		t.Errorf("Name: want %q, got %q", "Томат", got.Name)
	}
	if got.UnitID != unitID {
		t.Errorf("UnitID: want %d, got %d", unitID, got.UnitID)
	}
	if got.Description == nil || *got.Description != desc {
		t.Errorf("Description: want %q, got %v", desc, got.Description)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt: zero value, БД должна ставить now()")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt: zero value")
	}
	if got.ArchivedAt != nil {
		t.Errorf("ArchivedAt: want nil, got %v", got.ArchivedAt)
	}
}

// ---------------------------------------------------------------------------
// 2. Nullable: description nil / non-nil.
// ---------------------------------------------------------------------------

func TestCreateGet_NilDescription(t *testing.T) {
	ctx, repo := newTestRepo(t)
	unitID := seedUnit(ctx, t, repo)

	id := mustCreate(ctx, t, repo, CreateIngredientParams{
		Name: "Соль", UnitID: unitID,
	})

	got, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Description != nil {
		t.Errorf("Description: want nil, got %q", *got.Description)
	}
}

func TestCreateGet_WithDescription(t *testing.T) {
	ctx, repo := newTestRepo(t)
	unitID := seedUnit(ctx, t, repo)

	desc := "морская"
	id := mustCreate(ctx, t, repo, CreateIngredientParams{
		Name: "Соль2", UnitID: unitID, Description: &desc,
	})

	got, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Description == nil {
		t.Fatal("Description: want non-nil")
	}
	if *got.Description != desc {
		t.Errorf("Description: want %q, got %q", desc, *got.Description)
	}
}

// ---------------------------------------------------------------------------
// 3. Не найдено.
// ---------------------------------------------------------------------------

func TestGet_NotFound(t *testing.T) {
	ctx, repo := newTestRepo(t)

	_, err := repo.Get(ctx, 999_999_999)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, errIngredientNotFound) {
		t.Errorf("want errIngredientNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 4. Уникальность (требует UNIQUE INDEX ON lower(name) в миграции).
// ---------------------------------------------------------------------------

func TestCreate_UniqueCaseInsensitive(t *testing.T) {
	ctx, repo := newTestRepo(t)
	unitID := seedUnit(ctx, t, repo)

	if _, err := repo.Create(ctx, CreateIngredientParams{
		Name: "Мука", UnitID: unitID,
	}); err != nil {
		t.Fatalf("первый Create: %v", err)
	}

	_, err := repo.Create(ctx, CreateIngredientParams{
		Name: "мука", UnitID: unitID,
	})
	if err == nil {
		t.Fatal("want error unique violation, got nil")
	}
	if !errors.Is(err, errIngredientAlreadyExists) {
		t.Errorf("want errIngredientAlreadyExists, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 5. Список: алфавитный порядок + архивные не попадают.
// ---------------------------------------------------------------------------

func TestList_Alphabetical(t *testing.T) {
	ctx, repo := newTestRepo(t)
	unitID := seedUnit(ctx, t, repo)

	for _, n := range []string{"Морковь", "Арбуз", "Банан"} {
		mustCreate(ctx, t, repo, CreateIngredientParams{Name: n, UnitID: unitID})
	}

	list, err := repo.List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("want 3, got %d", len(list))
	}
	want := []string{"Арбуз", "Банан", "Морковь"}
	for i, w := range want {
		if list[i].Name != w {
			t.Errorf("position %d: want %q, got %q", i, w, list[i].Name)
		}
	}
}

func TestList_ExcludesArchived(t *testing.T) {
	ctx, repo := newTestRepo(t)
	unitID := seedUnit(ctx, t, repo)

	id1 := mustCreate(ctx, t, repo, CreateIngredientParams{Name: "Архивный", UnitID: unitID})
	id2 := mustCreate(ctx, t, repo, CreateIngredientParams{Name: "Живой", UnitID: unitID})

	if _, err := repo.db.Exec(ctx,
		`UPDATE ingredients SET archived_at = now() WHERE id = $1`, id1); err != nil {
		t.Fatalf("архивация: %v", err)
	}

	list, err := repo.List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var seenAlive, seenArchived bool
	for _, r := range list {
		if r.ID == id1 {
			seenArchived = true
		}
		if r.ID == id2 {
			seenAlive = true
		}
	}
	if seenArchived {
		t.Errorf("архивный id=%d не должен попадать в List", id1)
	}
	if !seenAlive {
		t.Errorf("живой id=%d должен быть в List", id2)
	}
}

// ---------------------------------------------------------------------------
// 6. Пагинация + отрицательные значения.
// ---------------------------------------------------------------------------

func TestList_Pagination(t *testing.T) {
	ctx, repo := newTestRepo(t)
	unitID := seedUnit(ctx, t, repo)

	for _, n := range []string{"А", "Б", "В", "Г", "Д"} {
		mustCreate(ctx, t, repo, CreateIngredientParams{Name: n, UnitID: unitID})
	}

	page1, err := repo.List(ctx, 2, 0)
	if err != nil {
		t.Fatalf("List(2,0): %v", err)
	}
	if len(page1) != 2 || page1[0].Name != "А" || page1[1].Name != "Б" {
		t.Errorf("page1: want [А Б], got %+v", page1)
	}

	page2, err := repo.List(ctx, 2, 2)
	if err != nil {
		t.Fatalf("List(2,2): %v", err)
	}
	if len(page2) != 2 || page2[0].Name != "В" || page2[1].Name != "Г" {
		t.Errorf("page2: want [В Г], got %+v", page2)
	}

	page3, err := repo.List(ctx, 2, 4)
	if err != nil {
		t.Fatalf("List(2,4): %v", err)
	}
	if len(page3) != 1 || page3[0].Name != "Д" {
		t.Errorf("page3: want [Д], got %+v", page3)
	}
}

func TestList_NegativeValues_Error(t *testing.T) {
	ctx, repo := newTestRepo(t)

	if _, err := repo.List(ctx, -1, 0); err == nil {
		t.Error("List(-1, 0): want error, got nil")
	}
	if _, err := repo.List(ctx, 10, -1); err == nil {
		t.Error("List(10, -1): want error, got nil")
	}
}

// ---------------------------------------------------------------------------
// 7. Пустой результат — не-nil слайс.
// ---------------------------------------------------------------------------

func TestList_Empty(t *testing.T) {
	ctx, repo := newTestRepo(t)

	list, err := repo.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list == nil {
		t.Error("want non-nil empty slice, got nil (JSON сериализуется в null)")
	}
	if len(list) != 0 {
		t.Errorf("want 0, got %d", len(list))
	}
}
