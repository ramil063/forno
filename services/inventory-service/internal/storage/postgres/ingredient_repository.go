package postgres

import "context"

// IngredientRepository - репозиторий для работы с ингредиентами
type IngredientRepository struct {
	db DBTX
}

// NewIngredientRepository - конструктор
func NewIngredientRepository(db DBTX) *IngredientRepository {
	return &IngredientRepository{
		db: db,
	}
}

// Create - создание ингредиента
func (r *IngredientRepository) Create(ctx context.Context, in CreateIngredientParams) (int64, error) {
	return CreateIngredient(ctx, r.db, in)
}

// Get - получение ингредиента
func (r *IngredientRepository) Get(ctx context.Context, id int64) (IngredientRow, error) {
	return GetIngredient(ctx, r.db, id)
}

// List - получение списка ингредиентов
func (r *IngredientRepository) List(ctx context.Context, limit, offset int) ([]IngredientRow, error) {
	return ListIngredients(ctx, r.db, limit, offset)
}
