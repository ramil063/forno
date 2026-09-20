-- +goose Up

INSERT INTO warehouses (name)
VALUES ('Основной склад');

-- +goose Down

DELETE
FROM warehouses
WHERE name = 'Основной склад';
