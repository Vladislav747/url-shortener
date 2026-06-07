# URL Shortener

HTTP-сервис для сокращения ссылок. Принимает длинный URL, сохраняет его в SQLite и возвращает короткий alias. По alias выполняет редирект на исходный адрес.

## Стек

- **Go** 1.25+
- **HTTP-роутер:** [chi](https://github.com/go-chi/chi)
- **Хранилище:** SQLite ([go-sqlite3](https://github.com/mattn/go-sqlite3))
- **Конфигурация:** YAML + переменные окружения ([cleanenv](https://github.com/ilyakaznacheev/cleanenv))
- **Логирование:** `slog` (pretty-формат для `local`, JSON для `dev`/`prod`)
- **SSO-клиент:** gRPC — см. раздел [gRPC / SSO](#grpc--sso)

## Структура проекта

```
.
├── cmd/url-shortener/          # Точка входа приложения
├── config/                     # YAML-конфиги
├── internal/
│   ├── clients/sso/grpc/       # gRPC-клиент SSO
│   ├── config/                 # Загрузка конфигурации
│   ├── http-server/
│   │   ├── handlers/           # HTTP-обработчики
│   │   └── middleware/         # Middleware (логирование)
│   ├── lib/                    # Общие утилиты
│   └── storage/sqlite/         # Реализация хранилища
├── tests/                      # Интеграционные тесты
└── deployment/                 # systemd unit для деплоя
```

## Требования

- Go 1.25 или новее
- CGO (нужен для `go-sqlite3`)

## Конфигурация

Приложение читает конфиг из YAML-файла. Путь задаётся переменной окружения `CONFIG_PATH`.

### Обязательные поля

| Поле | Описание |
|------|----------|
| `storage_path` | Путь к файлу SQLite |
| `http_server.user` | Логин для Basic Auth |
| `http_server.password` | Пароль для Basic Auth |
| `app_secret` | Секрет приложения |

### Переменные окружения

| Переменная | Описание |
|------------|----------|
| `CONFIG_PATH` | Путь к YAML-конфигу (обязательна) |
| `HTTP_SERVER_PASSWORD` | Пароль HTTP-сервера (перекрывает значение из YAML) |
| `APP_SECRET` | Секрет приложения (перекрывает значение из YAML) |

### Пример локального конфига

Создайте файл `config/local.yaml`:

```yaml
env: "local"
storage_path: "./storage.db"
app_secret: "local-secret"

http_server:
  address: "localhost:8082"
  timeout: 4s
  idle_timeout: 60s
  user: "myuser"
  password: "mypass"

clients:
  sso:
    address: "localhost:44044"
    timeout: 5s
    retries_count: 3
    insecure: true
```

## Запуск

```bash
# Установка зависимостей
go mod download

# Сборка
go build -o url-shortener ./cmd/url-shortener

# Запуск
CONFIG_PATH=./config/local.yaml ./url-shortener
```

Или без сборки:

```bash
CONFIG_PATH=./config/local.yaml go run ./cmd/url-shortener
```

## API

### Создать короткую ссылку

```http
POST /url
Authorization: Basic <credentials>
Content-Type: application/json

{
  "url": "https://example.com/some/long/path",
  "alias": "my-link"
}
```

Поле `alias` необязательно — если не передать, сгенерируется случайная строка из 6 символов.

**Успешный ответ (200):**

```json
{
  "status": "OK",
  "alias": "my-link"
}
```

**Пример запроса через curl:**

```bash
curl -X POST http://localhost:8082/url \
  -u myuser:mypass \
  -H "Content-Type: application/json" \
  -d '{"url": "https://example.com", "alias": "test"}'
```

### Редирект по alias

```http
GET /{alias}
```

При успехе возвращает `302 Found` с заголовком `Location`. Если alias не найден — JSON с ошибкой.

```bash
curl -L http://localhost:8082/test
```

## gRPC / SSO

Сервис подключается к внешнему SSO-микросервису по gRPC для проверки прав пользователей. Клиент находится в `internal/clients/sso/grpc/`.

### Зависимость

Proto-контракт и сгенерированный Go-код берутся из репозитория [`github.com/Vladislav747/protos`](https://github.com/Vladislav747/protos) (пакет `gen/go/sso`). Клиент использует интерфейс `ssov1.AuthClient`.

### Что делает клиент

При старте приложения создаётся gRPC-соединение с SSO-сервисом (`ssogrpc.New` в `main.go`). Клиент настраивается с двумя unary-интерцепторами:

1. **Логирование** — `grpclog.UnaryClientInterceptor` с адаптером `InterceptorLogger`, который прокидывает gRPC-логи в `slog`. Логируются события отправки и получения payload.
2. **Retry** — `grpcretry.UnaryClientInterceptor` с политикой повторов:
   - коды: `NotFound`, `Aborted`, `DeadlineExceeded`, `Internal`, `Unavailable`
   - количество попыток и таймаут на попытку задаются в конфиге

Соединение устанавливается без TLS (`insecure.NewCredentials()`) — подходит для локальной разработки. Для prod потребуется TLS.

### Доступные RPC-методы

| Метод | Описание |
|-------|----------|
| `IsAdmin` | Проверяет, является ли пользователь администратором |

В клиенте реализован метод `isAdmin(ctx, userID)`, который вызывает `IsAdmin` и возвращает `bool`. Пока метод не экспортирован и не используется в HTTP-обработчиках — интеграция в процессе.

### Конфигурация SSO-клиента

```yaml
clients:
  sso:
    address: "localhost:44044"   # адрес gRPC-сервера SSO
    timeout: 5s                  # таймаут одной попытки
    retries_count: 3           # число повторов при ошибке
    insecure: true               # без TLS (для local/dev)
```

| Поле | Описание |
|------|----------|
| `address` | Хост и порт SSO gRPC-сервера |
| `timeout` | Таймаут на одну попытку вызова |
| `retries_count` | Максимальное число повторов |
| `insecure` | Подключение без TLS |

### Локальный запуск с SSO

Для работы gRPC-части SSO-сервис должен быть запущен и доступен по адресу из конфига:

```bash
# 1. Запустить SSO-сервис (отдельный сервис, proto-контракт — в репозитории protos)
#    на порту, указанном в clients.sso.address

# 2. Запустить url-shortener
CONFIG_PATH=./config/local.yaml go run ./cmd/url-shortener
```

Если SSO-сервис недоступен, приложение упадёт при инициализации клиента (ошибка `grpc.DialContext`).

### Структура клиента

```
internal/clients/sso/grpc/
└── grpc.go          # Client, New(), isAdmin(), InterceptorLogger()
```

## Тесты

Интеграционные тесты ожидают запущенный сервер на `localhost:8082`:

```bash
CONFIG_PATH=./config/local.yaml go run ./cmd/url-shortener

# В другом терминале
go test ./tests/...
```

## Деплой

Деплой выполняется через GitHub Actions (`Deploy App`) на VM с systemd. Unit-файл лежит в `deployment/url-shortener.service`. На сервере используется `config/prod.yaml` и файл окружения `config.env` с `CONFIG_PATH` и `HTTP_SERVER_PASSWORD`.
