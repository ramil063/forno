package postgres

import (
	"errors"
)

var errEmptyDSN = errors.New("postgres: empty dsn")
var errIngredientNotFound = errors.New("postgres: ingredient not found")
var errIngredientAlreadyExists = errors.New("postgres: ingredient already exists")
