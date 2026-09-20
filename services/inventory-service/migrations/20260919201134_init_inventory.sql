-- +goose Up

-- 1. Единицы измерения
CREATE TABLE units
(
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       VARCHAR(50) NOT NULL UNIQUE,
    short_name VARCHAR(10) NOT NULL UNIQUE
);

INSERT INTO units (name, short_name)
VALUES ('миллилитр', 'мл'),
       ('грамм', 'г'),
       ('штука', 'шт');

COMMENT
ON TABLE  units            IS 'Справочник единиц измерения ингредиентов';
COMMENT
ON COLUMN units.id         IS 'Первичный ключ';
COMMENT
ON COLUMN units.name       IS 'Полное название единицы, например «грамм»';
COMMENT
ON COLUMN units.short_name IS 'Сокращённое обозначение, например «г»';

-- 2. Склады
CREATE TABLE warehouses
(
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        VARCHAR(100) NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ NULL
);

COMMENT
ON TABLE  warehouses            IS 'Склады, на которых хранятся ингредиенты';
COMMENT
ON COLUMN warehouses.id         IS 'Первичный ключ';
COMMENT
ON COLUMN warehouses.name       IS 'Название склада';
COMMENT
ON COLUMN warehouses.created_at IS 'Дата и время создания записи';
COMMENT
ON COLUMN warehouses.updated_at IS 'Дата и время последнего изменения';
COMMENT
ON COLUMN warehouses.archived_at IS 'Дата архивации';

-- 3. Ингредиенты
CREATE TABLE ingredients
(
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name            VARCHAR(200) NOT NULL,
    name_normalized VARCHAR(200)
        GENERATED ALWAYS AS (lower(trim(name))) STORED,
    unit_id         BIGINT       NOT NULL REFERENCES units (id) ON DELETE RESTRICT,
    description     TEXT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    archived_at     TIMESTAMPTZ NULL
);

CREATE UNIQUE INDEX uq_ingredients_name_normalized
    ON ingredients (name_normalized)
    WHERE archived_at IS NULL;

CREATE INDEX idx_ingredients_unit_id ON ingredients (unit_id);
CREATE INDEX idx_ingredients_name_pattern ON ingredients (name text_pattern_ops);

COMMENT
ON TABLE  ingredients                 IS 'Справочник ингредиентов';
COMMENT
ON COLUMN ingredients.id              IS 'Первичный ключ';
COMMENT
ON COLUMN ingredients.name            IS 'Название ингредиента в том виде, как его ввёл пользователь';
COMMENT
ON COLUMN ingredients.name_normalized IS 'Нормализованное имя: lower(trim(name))';
COMMENT
ON COLUMN ingredients.unit_id         IS 'Ссылка на единицу измерения (units.id)';
COMMENT
ON COLUMN ingredients.description     IS 'Произвольное описание ингредиента';
COMMENT
ON COLUMN ingredients.created_at      IS 'Дата и время создания записи';
COMMENT
ON COLUMN ingredients.updated_at      IS 'Дата и время последнего изменения';
COMMENT
ON COLUMN ingredients.archived_at      IS 'Дата архивации';

COMMENT
ON INDEX uq_ingredients_name_normalized IS 'Регистронезависимая уникальность имени ингредиента';

-- 4. Остатки (quantity = физический остаток)
CREATE TABLE ingredient_stock
(
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    warehouse_id      BIGINT      NOT NULL REFERENCES warehouses (id) ON DELETE RESTRICT,
    ingredient_id     BIGINT      NOT NULL REFERENCES ingredients (id) ON DELETE RESTRICT,
    reserved_quantity BIGINT      NOT NULL DEFAULT 0 CHECK (reserved_quantity >= 0),
    quantity          BIGINT      NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- reserved_quantity не может превышать физический остаток
    CONSTRAINT chk_stock_quantity_ge_reserved_quantity CHECK (quantity >= reserved_quantity),

    -- один остаток на пару «склад + ингредиент»
    CONSTRAINT uq_stock_warehouse_ingredient UNIQUE (warehouse_id, ingredient_id)
);

CREATE INDEX idx_stock_ingredient ON ingredient_stock (ingredient_id);

COMMENT
ON TABLE  ingredient_stock               IS 'Остатки ингредиентов по складам';
COMMENT
ON COLUMN ingredient_stock.id            IS 'Первичный ключ';
COMMENT
ON COLUMN ingredient_stock.warehouse_id  IS 'Ссылка на склад (warehouses.id)';
COMMENT
ON COLUMN ingredient_stock.ingredient_id IS 'Ссылка на ингредиент (ingredients.id)';
COMMENT
ON COLUMN ingredient_stock.quantity     IS 'Физическое количество на складе. Доступное для новых резервов = quantity - reserved_quantity';
COMMENT
ON COLUMN ingredient_stock.reserved_quantity       IS 'Забронированное количество. Не может превышать quantity';
COMMENT
ON COLUMN ingredient_stock.updated_at    IS 'Дата и время последнего изменения';

COMMENT
ON CONSTRAINT chk_stock_quantity_ge_reserved_quantity ON ingredient_stock
    IS 'Резерв не может превышать физический остаток';
COMMENT
ON CONSTRAINT uq_stock_warehouse_ingredient  ON ingredient_stock
    IS 'Один остаток на пару «склад + ингредиент»';

-- Универсальная функция для простановки updated_at
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
COMMENT
ON FUNCTION set_updated_at() IS
    'BEFORE UPDATE триггер: проставляет NEW.updated_at = now(). Используется на таблицах с колонкой updated_at';

CREATE TRIGGER trg_warehouses_updated_at
    BEFORE UPDATE
    ON warehouses
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER trg_ingredients_updated_at
    BEFORE UPDATE
    ON ingredients
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER trg_ingredient_stock_updated_at
    BEFORE UPDATE
    ON ingredient_stock
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- +goose Down

DROP TRIGGER IF EXISTS trg_ingredient_stock_updated_at ON ingredient_stock;

DROP TABLE IF EXISTS ingredient_stock;
DROP TABLE IF EXISTS ingredients;
DROP TABLE IF EXISTS warehouses;
DROP TABLE IF EXISTS units;

DROP FUNCTION IF EXISTS set_updated_at();
