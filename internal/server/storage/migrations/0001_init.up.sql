CREATE TABLE IF NOT EXISTS users (
    id             UUID PRIMARY KEY,
    login          TEXT NOT NULL UNIQUE,
    password_hash  TEXT NOT NULL,
    kek_salt       BYTEA NOT NULL DEFAULT '',
    dek_ciphertext BYTEA NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS items (
    id          UUID PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type        TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    login       TEXT NOT NULL DEFAULT '',
    password    TEXT NOT NULL DEFAULT '',
    data        BYTEA NOT NULL DEFAULT '',
    card_number TEXT NOT NULL DEFAULT '',
    card_exp    TEXT NOT NULL DEFAULT '',
    card_cvv    TEXT NOT NULL DEFAULT '',
    meta        TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS items_user_id_idx ON items (user_id);
