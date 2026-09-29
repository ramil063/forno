// Заглушка этапа 0 — проверяем, что окружение работает.
// Настоящий сервис появится на этапе 1 вместе с proto и gRPC.
package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/ramil063/forno/services/inventory-service/internal/checkenv"
	"github.com/ramil063/forno/services/inventory-service/internal/meta"
	"github.com/ramil063/forno/services/inventory-service/internal/storage/postgres"
)

const checkTimeout = 10 * time.Second

func main() {
	fmt.Printf("%s запущен на Go %s\n", meta.Name, runtime.Version())

	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()

	results, err := checkenv.Run(ctx, postgres.Config{
		DSN:              os.Getenv("POSTGRES_DSN"),
		ApplicationName:  meta.Name,
		StatementTimeout: 5 * time.Second,
	})

	for _, r := range results {
		if r.Err == nil {
			fmt.Printf("ok %s\n", r.Name)
			continue
		}
		fmt.Printf("FAIL  %s: %v\n", r.Name, r.Err)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "\nпроверка окружения провалена:\n%v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nокружение в порядке")
}
