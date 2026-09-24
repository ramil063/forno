// Заглушка этапа 0 — проверяем, что окружение работает.
// Настоящий сервис появится на этапе 1 вместе с proto и gRPC.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/ramil063/forno/services/inventory-service/internal/meta"
	"github.com/ramil063/forno/services/inventory-service/internal/storage/postgres"
)

func main() {
	fmt.Printf("%s запущен на Go %s\n", meta.Name, runtime.Version())

	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	pool, err := postgres.New(ctx, postgres.Config{
		DSN:              os.Getenv("POSTGRES_DSN"),
		ApplicationName:  meta.Name,
		StatementTimeout: 5 * time.Second,
	})
	if err != nil {
		return err
	}
	defer pool.Close()

	log.Println("postgres: ok")

	var appName, stmt string
	err = pool.QueryRow(ctx,
		"SELECT current_setting('application_name'), current_setting('statement_timeout')").
		Scan(&appName, &stmt)
	if err != nil {
		return err
	}
	log.Printf("база: application_name=%s statement_timeout=%s", appName, stmt)

	return nil
}
