CREATE TABLE IF NOT EXISTS files (
    id         UUID PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       TEXT NOT NULL DEFAULT '',
    size       BIGINT NOT NULL DEFAULT 0,
    meta       TEXT NOT NULL DEFAULT '',
    s3_key     TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS files_user_id_idx ON files (user_id);
