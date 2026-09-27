-- name: CreateUser :one
INSERT INTO users (id, login, password_hash, kek_salt, dek_ciphertext)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, login, password_hash, kek_salt, dek_ciphertext, created_at;

-- name: GetUserByLogin :one
SELECT id, login, password_hash, kek_salt, dek_ciphertext, created_at
FROM users
WHERE login = $1;

-- name: GetUserByID :one
SELECT id, login, password_hash, kek_salt, dek_ciphertext, created_at
FROM users
WHERE id = $1;

-- name: CreateItem :one
INSERT INTO items (id, user_id, type, name, login, password, data, card_number, card_exp, card_cvv, meta, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING id, user_id, type, name, login, password, data, card_number, card_exp, card_cvv, meta, updated_at;

-- name: GetItem :one
SELECT id, user_id, type, name, login, password, data, card_number, card_exp, card_cvv, meta, updated_at
FROM items
WHERE id = $1 AND user_id = $2;

-- name: ListItems :many
SELECT id, user_id, type, name, login, password, data, card_number, card_exp, card_cvv, meta, updated_at
FROM items
WHERE user_id = $1
ORDER BY updated_at DESC;

-- name: DeleteItem :execrows
DELETE FROM items
WHERE id = $1 AND user_id = $2;
