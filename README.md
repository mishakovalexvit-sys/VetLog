# Ветлог

Лендинг с формой заявки для ветклиник. Go 1.23+, net/http, html/template, PostgreSQL, Caddy.

## Запуск

```bash
cp .env.example .env     # один раз: заполните DOMAIN, POSTGRES_PASSWORD и SMTP_* (BASE_URL уже указан как пример)
docker compose up -d
```

Caddy получит TLS-сертификат для `DOMAIN` автоматически. Порты 80 и 443 должны быть открыты, а DNS-запись домена должна указывать на сервер.

Логи в JSON: `docker compose logs -f app`.

## Схема

```
Клиент ──HTTPS──► Caddy ──HTTP──► app (distroless, nonroot) ──► PostgreSQL (volume pgdata)
                                      │
                                      └──► очередь писем (канал, 1 воркер, 3 повтора) ──► SMTP (587, STARTTLS)
```

Страницы:

| Путь | Описание |
|---|---|
| `/`, `/en/` | Лендинг (ru / en), `hreflang` и canonical |
| `/privacy`, `/en/privacy` | Политика конфиденциальности из ключей `privacy.title` и `privacy.body`. Если ключей нет в словаре, показывается заглушка «текст в подготовке» |
| `POST /lead`, `POST /en/lead` | Отправка заявки |
| `GET /healthz` | Проверка для контейнера (проверяет БД) |

## Переменные окружения

| Переменная | По умолчанию | Описание |
|---|---|---|
| `DATABASE_URL` | — | Строка подключения. В compose собирается автоматически из `POSTGRES_PASSWORD` |
| `HTTP_ADDR` | `:8080` | Адрес HTTP-сервера |
| `HEALTH_URL` | строится из `HTTP_ADDR` | URL для команды `server healthcheck`. Нужен, только если адрес нестандартный или healthcheck идёт снаружи контейнера. Если не задан, `:9000` даёт `http://127.0.0.1:9000/healthz`, `0.0.0.0:9000` тоже |
| `BASE_URL` | `http://localhost:8080` | Публичный адрес для canonical и hreflang, например `https://vetlog.example.ru` |
| `MIGRATE_ON_START` | `true` | Применять миграции при старте. `false` отключает |
| `COOKIE_SECURE` | `true` | Флаг Secure у CSRF-cookie. Для локального HTTP поставьте `false` |
| `TRUST_PROXY` | `false` в коде, `true` в `.env.example` | Брать IP клиента из X-Forwarded-For. В `.env.example` стоит `true`, потому что compose работает за Caddy: Caddy дописывает реальный IP в конец заголовка, и приложение берёт последний адрес. Для локального запуска без прокси поставьте `false`, иначе клиент сможет подменить IP и обойти лимит |
| `LOG_LEVEL` | `info` | Уровень логов slog: debug, info, warn, error |
| `SMTP_HOST` | пусто | Сервер SMTP. Пусто — письма не отправляются, а логируются |
| `SMTP_PORT` | `587` | Только 587 (STARTTLS). Значение 465 отклоняется при старте |
| `SMTP_USER`, `SMTP_PASSWORD` | пусто | Учётные данные SMTP (секреты только в `.env`) |
| `SMTP_FROM`, `SMTP_TO` | пусто | Адрес отправителя и получатели через запятую |
| `POSTGRES_PASSWORD` | — | Пароль БД (compose) |
| `DOMAIN` | — | Домен для Caddy (compose) |

## SMTP

Письма уходят через submission-порт **587** с обязательным **STARTTLS**: приложение открывает соединение, выполняет `STARTTLS`, и только потом передаёт учётные данные. Если сервер не предлагает STARTTLS, письмо не отправляется, в лог пишется ошибка.

**Порт 465 (неявный TLS, SMTPS) не поддерживается.** Значение `SMTP_PORT=465` приложение отклоняет при старте. Если провайдер требует 465, нужен отдельный код или переход на 587.

## Схема БД

`leads`: заявка (имя, телефон, клиника, email необязательный, язык страницы, время).
`lead_consents`: согласие на обработку (время, IP, User-Agent), связь 1:1 с заявкой.
Заявка и согласие пишутся в одной транзакции. Миграции лежат в `migrations/` и применяются в одной транзакции с advisory-lock, поэтому несколько реплик не конфликтуют.

## Безопасность формы

- CSRF: double-submit cookie (`vetlog_csrf`, HttpOnly, SameSite=Strict), сравнение в постоянное время.
- Honeypot: скрытое поле `website`. Бот получает обычный редирект, но заявка не сохраняется.
- Rate limit: 5 попыток отправки в минуту с одного IP. Считаются все POST, включая невалидные.
- Валидация на сервере. Ошибки выводятся на языке страницы из `internal/i18n/locales`.
- Текст согласия содержит ссылку на политику конфиденциальности (`/privacy` или `/en/privacy`).

## Локализация

Ключи в `internal/i18n/locales/ru.yaml` и `en.yaml` должны совпадать. Если ключа нет в одном из словарей, `i18n.Load()` возвращает ошибку, приложение не стартует, а тест `TestEmbeddedDictionariesAreComplete` падает в CI.

Словари пишутся вложенным YAML (`form.consent.before` = `form: consent: before:`). Загрузчик превращает их в плоские ключи через точку. Тест `TestTemplateKeysExist` проверяет, что каждый ключ из шаблонов и обработчика есть в обоих словарях.

## Разработка

```bash
go mod tidy                                          # создаст go.sum (закоммитьте его)
go vet ./...
go test -race ./...                                  # unit-тесты
go test -race -tags=integration ./internal/lead/... # нужен Docker (testcontainers)
golangci-lint run
docker build -t vetlog .
```

## Структура

```
cmd/server            точка входа, graceful shutdown, режим healthcheck
internal/config       чтение env, проверка SMTP_PORT
internal/db           применение миграций
internal/handler      HTTP-обработчики, CSRF, rate limit, рендер, политика
internal/i18n         словари и перевод
internal/lead         модель, валидация, репозиторий PostgreSQL
internal/mail         SMTP (STARTTLS), очередь с повторами
migrations            SQL-файлы (embed)
web                   шаблоны и статика (embed)
deploy                Caddyfile
```

## Открытые вопросы для деплоя

- Email для заявок и SMTP-данные (`SMTP_HOST`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM`, `SMTP_TO`). Пока не заданы, письма только логируются.
- Домен: `DOMAIN` и `BASE_URL`.
- Юридическая проверка текста политики (`privacy.*`) и согласия (`form.consent.*`).
