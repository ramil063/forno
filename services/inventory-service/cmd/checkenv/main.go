// Команда checkenv проверяет окружение: база, миграции, пул.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ramil063/forno/services/inventory-service/internal/checkenv"
	"github.com/ramil063/forno/services/inventory-service/internal/meta"
	"github.com/ramil063/forno/services/inventory-service/internal/storage/postgres"
)

const checkTimeout = 10 * time.Second

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "\nпроверка окружения провалена:\n%v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	results, err := checkenv.Run(ctx, postgres.Config{
		DSN:              os.Getenv("POSTGRES_DSN"),
		ApplicationName:  meta.Name,
		StatementTimeout: 5 * time.Second,
	})
	for _, r := range results {
		switch {
		case r.Skipped:
			fmt.Printf("SKIP  %s: отменена из-за другой проверки\n", r.Name)
		case r.Err == nil:
			fmt.Printf("ok %s\n", r.Name)
			if r.Detail != "" {
				fmt.Printf("detail %s\n", r.Detail)
			}
		default:
			fmt.Printf("FAIL  %s: %v\n", r.Name, r.Err)
		}
	}
	return err
}
