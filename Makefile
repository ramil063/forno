COMPOSE := docker compose
TOOLS := $(COMPOSE) run --rm -T tools

.DEFAULT_GOAL := help

.PHONY: help image shell run hooks db-up db-down db-logs psql build test test-race vet fmt fmt-diff lint lint-fix versions ps clean migrate-up migrate-down migrate-status migrate-create

help:
	@echo "forno — полезные команды:"
	@echo "  make image      собрать образ с инструментами (один раз, ~5 минут)"
	@echo "  make shell      зайти в контейнер с исходниками"
	@echo "  make run        запустить inventory-service (внутри контейнера)"
	@echo "  make hooks      подключить git-хуки (один раз после клонирования)"
	@echo "  make db-up      поднять PostgreSQL (при первом запуске создаст .env)"
	@echo "  make db-down    остановить контейнеры"
	@echo "  make db-logs    смотреть логи PostgreSQL"
	@echo "  make psql       открыть psql внутри контейнера"
	@echo "  make migrate-up       накатить миграции"
	@echo "  make migrate-down     откатить последнюю миграцию"
	@echo "  make migrate-status   что накатано, что нет"
	@echo "  make migrate-create name=add_ingredients   создать пустую миграцию"
	@echo "  make build      go build ./..."
	@echo "  make test       go test ./..."
	@echo "  make test-race  go test -race ./... (тесты на гонки)"
	@echo "  make vet        go vet ./..."
	@echo "  make fmt        форматирование: отступы (gofmt) + порядок импортов (gci)"
	@echo "  make fmt-diff   показать, что не отформатировано, ничего не меняя"
	@echo "  make lint       golangci-lint run ./..."
	@echo "  make lint-fix   то же, но с автоматическим исправлением"
	@echo "  make versions   версии Go и инструментов внутри образа"
	@echo "  make ps         статус контейнеров"
	@echo "  make start      старт контейнеров"
	@echo "  make stop       стоп контейнеров"
	@echo "  make clean      удалить артефакты сборки"

image:
	$(COMPOSE) build tools

shell:
	$(COMPOSE) run --rm tools bash

# Запуск сервиса внутри контейнера: Go, переменные окружения и адрес базы там же,
# где у остальных make-целей. Из IDE это не заработает — там свой тулчейн хоста.
run:
	$(TOOLS) sh -c 'cd services/inventory-service && go run ./cmd'

# Хуки лежат в репозитории, но включаются локально — поэтому этот вызов нужен один раз.
# pre-commit и pre-push не дают коммитить и пушить прямо в main.
hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-commit .githooks/pre-push
	@echo "хуки подключены: правки в main теперь идут только через ветку и PR"

# Локальный файл с паролями в git не хранится — создаём его из шаблона
.env:
	cp .env.example .env
	@echo "создан .env из .env.example — пароль при желании поменяй"

# Без .env docker compose не подставит порты и доступы к базе, поэтому он нужен всем
# целям, которые заходят в compose — иначе первый же make после клонирования упадёт
image shell run build test test-race vet fmt fmt-diff lint lint-fix versions ps db-up db-down db-logs psql migrate-up migrate-down migrate-status migrate-create: .env

db-up: .env
	$(COMPOSE) up -d postgres
	@echo "PostgreSQL поднимается, порт смотри в .env"

db-down:
	$(COMPOSE) down

db-logs: .env
	$(COMPOSE) logs -f postgres

# psql внутри контейнера: переменные окружения там уже есть, пароль не светим снаружи
psql: .env
	$(COMPOSE) exec postgres sh -c 'psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"'

# Миграции inventory-service. Строку подключения goose берёт из POSTGRES_DSN —
# внутрь контейнера её подставляет compose, адрес уже внутрисетевой (postgres:5432).
# Каталог миграций пока один; когда миграции появятся у других сервисов,
# здесь появятся отдельные цели или параметр с именем сервиса.
MIGRATIONS_DIR := services/inventory-service/migrations

migrate-up:
	$(TOOLS) sh -c 'goose -dir $(MIGRATIONS_DIR) postgres "$$POSTGRES_DSN" up'

# goose down откатывает ровно одну миграцию — последнюю
migrate-down:
	$(TOOLS) sh -c 'goose -dir $(MIGRATIONS_DIR) postgres "$$POSTGRES_DSN" down'

migrate-status:
	$(TOOLS) sh -c 'goose -dir $(MIGRATIONS_DIR) postgres "$$POSTGRES_DSN" status'

# make migrate-create name=add_ingredients — goose добавит файл с номером и шаблоном Up/Down
migrate-create:
	@if [ -z "$(name)" ]; then echo "укажи имя: make migrate-create name=add_ingredients"; exit 1; fi
	$(TOOLS) sh -c 'mkdir -p $(MIGRATIONS_DIR) && goose -dir $(MIGRATIONS_DIR) create $(name) sql'

# go.work в корне не даёт использовать ./... из корня: Go ругается, что префикс
# каталога не содержит модулей воркспейса. Поэтому обходим модули по одному.
define for_each_module
$(TOOLS) sh -c 'set -e; for m in $$(find . -maxdepth 3 -name go.mod -not -path "./.git/*"); do d=$${m%/go.mod}; echo "== $$d"; (cd "$$d" && $(1)); done'
endef

build:
	$(call for_each_module,go build ./...)

test:
	$(call for_each_module,go test ./...)

test-race:
	$(call for_each_module,go test -race ./...)

vet:
	$(call for_each_module,go vet ./...)

# golangci-lint читает .golangci.yml в корне: gofmt + gci, импорты тремя группами
fmt:
	$(call for_each_module,golangci-lint fmt)

# Показать, что не отформатировано, ничего не меняя — для проверки перед коммитом
fmt-diff:
	$(call for_each_module,golangci-lint fmt --diff)

lint:
	$(call for_each_module,golangci-lint run ./...)

# То же, но с автоматическим исправлением того, что можно исправить
lint-fix:
	$(call for_each_module,golangci-lint run --fix ./...)

versions:
	$(TOOLS) sh -c 'go version && buf --version && protoc-gen-go --version && protoc-gen-go-grpc --version && mockery --version && golangci-lint --version && air -v && goose -version'

ps:
	$(COMPOSE) ps

clean:
	rm -rf bin tmp coverage.out coverage.html

start:
	$(COMPOSE) start

stop:
	$(COMPOSE) stop
