# lekalo-postgres

Языконезависимый PostgreSQL storage provider для [Lekalo](https://github.com/ichinya/lekalo), общий для PHP, TypeScript, Go и Rust.

## Статус

Репозиторий подготовлен для выделения существующей реализации. Код ещё не перенесён; пакет пока не готов к установке или использованию.

- Перенос реализации: [#1](https://github.com/ichinya/lekalo-postgres/issues/1).
- Общий план: [lekalo#241](https://github.com/ichinya/lekalo/issues/241).

## Ответственность пакета

- PostgreSQL-specific отображение типов и quoting идентификаторов.
- Матрица возможностей и версий PostgreSQL, engine policies и capability snapshots.
- Детерминированная генерация PostgreSQL DDL и специфичные правила миграций/drift.

## Источник переноса

`lekalo/crates/lekalo-core/src/storage_engine/postgres/{ddl,quoting,types,snapshot,version_matrix}.rs` и PostgreSQL-specific ветки storage drift/conformance.

## Границы

Общие storage contracts, Model/IR, проверка evidence, permissions, credentials и разрешение применения плана остаются у host. Пакет не получает право исполнять SQL из-за генерации DDL. ORM bindings принадлежат Laravel/Drizzle и другим framework-пакетам.

Версия пакета, версия provider/protocol API и версия языка или движка приложения фиксируются отдельно. Сборка и тесты должны быть автономными; источники и зависимости закрепляются точными версиями и digest. Выбор произвольного совместимого Git/local package и загрузка его зависимостей отслеживаются в [lekalo#239](https://github.com/ichinya/lekalo/issues/239).
