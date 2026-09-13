// Заглушка этапа 0 — проверяем, что окружение работает.
// Настоящий сервис появится на этапе 1 вместе с proto и chi.
package main

import (
	"fmt"
	"runtime"

	"github.com/ramil063/forno/services/order-service/internal/meta"
)

func main() {
	fmt.Printf("%s запущен на Go %s\n", meta.Name, runtime.Version())
}
