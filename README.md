# GophKeeper

MVP менеджера паролей и секретов: gRPC-сервер на Go, CLI-клиент, PostgreSQL.

## Возможности

- регистрация/логин (bcrypt + JWT в metadata `authorization`);
- хранение секретов четырёх типов: логин/пароль, текст, бинарные данные, банковская карта;
- доступ к данным только владельца (проверка `user_id` на уровне storage);
- gRPC API: `Register`, `Login`, `Ping`, `CreateItem`, `GetItem`, `ListItems`, `DeleteItem`;
- серверное шифрование секретов по схеме KEK/DEK: из мастер-пароля Argon2id-ом выводится KEK (per-user соль), которым AES-256-GCM шифруется случайный DEK; DEK шифрует чувствительные поля items (`login`, `password`, `data`, `card_number`, `card_exp`, `card_cvv`, `meta`; `name` и `type` остаются открытыми для листинга). Мастер-пароль серверу задаётся через `GOPHKEEPER_MASTER_PASSWORD` (или флаг `-master-password`), живёт только в памяти сервера и нигде не логируется — при его утере расшифровать существующие секреты невозможно;
- прото-файл: `proto/gophkeeper/v1/gophkeeper.proto`.

## Запуск (docker compose)

```bash
docker compose up --build
```

Поднимаются два сервиса:
- `postgres:16-alpine` (db/user/password: `gophkeeper`, volume `pgdata`, healthcheck `pg_isready`);
- `server` — gRPC на `:3200`, миграции применяются автоматически при старте.

Секреты `JWT_SECRET`, `GOPHKEEPER_MASTER_PASSWORD` и пароль БД для продакшена задаются через env/секреты, значения
в `docker-compose.yaml` — только для локальной разработки.

## Клиент

Сборка: `make build` → `bin/gophkeeper-client` (версия/коммит/дата — через ldflags, см. `make build`).

Адрес сервера: флаг `-a` или `GOPHKEEPER_ADDR` (по умолчанию `localhost:3200`).
Токен сохраняется в `~/.gophkeeper/token.json` (права 0600) после `register`/`login`.

```bash
gophkeeper register alice s3cr3t          # создать пользователя и сохранить токен
gophkeeper login alice s3cr3t             # войти
gophkeeper ping                           # проверка связи и токена
gophkeeper items create --type LOGIN --name "github" --login bob --password qwerty --meta "work"
gophkeeper items create --type CARD --name "visa" --card-number 4242... --card-exp 12/30 --card-cvv 123
gophkeeper items create --type BINARY --name "key" --data @~/.ssh/id_rsa
gophkeeper items list
gophkeeper items get <id>
gophkeeper items delete <id>
gophkeeper version
```

### Локальный кэш и офлайн-режим

Команды `items list` и `items get` кэшируют результат в локальной базе SQLite (`~/.gophkeeper/cache.db`, путь можно переопределить через `GOPHKEEPER_CACHE`; файл создаётся с правами 0600). Кэш использует pure-Go драйвер `modernc.org/sqlite` (без cgo), поэтому клиент кроссплатформенный. Строчный стриминг кэша реализован через `iter.Seq` (`cache.Cache.ListItemsSeq`).

Клиентский конфиг собирается через функциональные опции (`client.NewConfig`, `client.WithAddress/WithTokenPath/WithCachePath/WithTimeout`, `client.WithEnv`), env-значения читаются через переданную функцию (по умолчанию `os.Getenv`). Таймаут вызова: `GOPHKEEPER_TIMEOUT` (Go-дюрация, по умолчанию 60s).

Если сервер недоступен (нет сети, `codes.Unavailable`, истёк таймаут вызова), команды читают данные из кэша и печатают в stderr предупреждение `warning: server unavailable, showing cached data`. Если в кэше данных нет — ошибка `server unavailable and no cached data`.

Офлайн-режим read-only: `items create` и `items delete` при недоступном сервере завершаются ошибкой `server unavailable` (офлайн-очередь изменений — следующий этап; шифрование полей кэша ключом из пароля пользователя — тоже).

## Файлы и S3

Большие бинарные файлы хранятся не в `items.data`, а в S3-совместимом объектном хранилище (MinIO в docker-compose), передача идёт через gRPC streaming по чанкам — файл никогда не собирается целиком ни в памяти сервера, ни в памяти клиента (чанк 64 KiB). Каждый чанк шифруется AES-256-GCM тем же per-user DEK, что и секреты items; в объекте лежит последовательность фреймов `4-byte BE length || nonce || ciphertext`. Ключ объекта: `files/<userID>/<fileID>`. Доступ только владельца (проверка `user_id` в таблице `files`); при неудачной записи/загрузке объект из S3 удаляется (компенсация). Ошибка расшифрования → `codes.Internal` с упоминанием, что мастер-пароль сервера может не совпадать с использованным при загрузке.

Env сервера: `S3_ENDPOINT` (по умолчанию `http://minio:9000`), `S3_BUCKET` (`gophkeeper`), `S3_ACCESS_KEY`/`S3_SECRET_KEY`. Бакет создаётся автоматически при старте (`EnsureBucket`: HeadBucket → CreateBucket).

Команды клиента:

```bash
gophkeeper files upload ./photo.jpg --name "photo.jpg" --meta "holiday"   # напечатает id
gophkeeper files download <id> -o out.jpg                                # дефолт — имя из meta
gophkeeper files list
gophkeeper files delete <id>
```

Файлы и их метаданные не кэшируются локально (MVP): при недоступном сервере команды `files *` возвращают ошибку `server unavailable`.

## Сервер (локально без docker)

```bash
DATABASE_URL="postgres://gophkeeper:gophkeeper@localhost:5432/gophkeeper?sslmode=disable" \
JWT_SECRET="dev-secret" \
GOPHKEEPER_MASTER_PASSWORD="dev-master" \
go run ./cmd/server
```

Флаги: `-d` (DSN), `-s` (JWT secret), `-a` (адрес, по умолчанию `:3200`), `-master-password` (мастер-пароль шифрования); env: `DATABASE_URL`, `JWT_SECRET`, `GOPHKEEPER_MASTER_PASSWORD`, `GOPHKEEPER_TOKEN_TTL` (время жизни JWT, по умолчанию 24h).

## Разработка

```bash
make build   # сборка server + client с ldflags-версией
make test    # go test -race ./...
make lint    # golangci-lint run
make proto   # регенерация *.pb.go через protoc в docker
```

Первый запуск: `go mod tidy` (go.sum уже в репозитории).

## Что дальше (не вошло в MVP)

- TLS для gRPC;
- OPAQUE / SRP вместо передачи пароля;
- шифрование секретов на стороне клиента;
- S3-хранилище и чанкинг для больших бинарных данных;
- локальный sqlite-кэш реализован; офлайн-очередь изменений — следующий этап.
