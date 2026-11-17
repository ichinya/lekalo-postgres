# lekalo-postgres

PostgreSQL storage provider, independent of application language and of the Lekalo executable. Owns the version matrix (15–18), type policies, deterministic DDL, defaults, sequence/index names and capability answers. It never opens a database connection or executes SQL.

Build/check: `node scripts/build.mjs`, `node scripts/build.mjs --check`, `node --test test/*.test.mjs`.

The zero-dependency `bin/lekalo-postgres.mjs` speaks `lekalo/runtime/v1` over bounded stdin/stdout. `lekalo.package.json` lets the host resolve this repository by a pinned Git reference or a local package path. Runtime source selection and contract-rule selection stay independent.

The current legacy storage CLI selects its provider through `LEKALO_STORAGE_PROVIDER`, encoded as a JSON Source:

```sh
export LEKALO_STORAGE_PROVIDER='"path:/absolute/path/lekalo-postgres"'
# Or an independently installed executable/bundle:
export LEKALO_STORAGE_PROVIDER='{"command":["node","/absolute/path/bin/lekalo-postgres.mjs"]}'
```

The host validates its model/projection binding, sends data, confines the process and validates the reply. There is no library linkage to this repository and no embedded SQL fallback. Missing/malformed providers fail closed. The legacy default executable name is `lekalo-postgres`.

Operations: `storage.matrix`, `storage.type`, `storage.ddl`, `storage.table`, `storage.join`, `storage.default`, `storage.sequence`, `storage.index-name`. Each result is `{value:...}` or `{error:{kind:"mapping"|"render",detail:...}}`, echoed in the standard response envelope. Types, defaults, indexes and derived tables are JSON data; integer literals cross as decimal strings to preserve i64 precision. `describe` advertises the operation set. `check` accepts no contract rules: this storage provider does not claim to analyze application source code.

The legacy host still owns model admission, migration orchestration and drift comparison. Their existing calls now use this provider for the extracted DDL primitives; further removal of PostgreSQL-specific migration/drift policy is tracked separately. Do not interpret this extraction as completion of the entire storage plugin API.
