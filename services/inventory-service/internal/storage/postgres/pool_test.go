package postgres

import (
	"context"
	"errors"
	"testing"
)

func TestNew_EmptyDSN(t *testing.T) {
	_, err := New(context.Background(), Config{DSN: "   "})
	if !errors.Is(err, errEmptyDSN) {
		t.Fatalf("expected errEmptyDSN, got %v", err)
	}
}

func TestNew_InvalidDSN(t *testing.T) {
	_, err := New(context.Background(), Config{DSN: "://not-a-valid-dsn"})
	if err == nil {
		t.Fatal("ожидал ошибку для неверного DSN, получил nil")
	}
}

func TestNew_DBUnreachable(t *testing.T) {
	_, err := New(context.Background(), Config{DSN: "postgres://user:pass@127.0.0.1:1/db"})
	if err == nil {
		t.Fatal("ожидал ошибку: по этому адресу базы нет")
	}
}

func TestNew_NegativeMaxConns(t *testing.T) {
	_, err := New(context.Background(), Config{DSN: "postgres://user:pass@127.0.0.1:1/db", MaxConns: -1})
	if err == nil {
		t.Fatal("ожидал ошибку валидации для отрицательного MaxConns")
	}
}
