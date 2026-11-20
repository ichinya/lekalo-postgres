# lekalo-postgres

PostgreSQL storage provider, independent of application language and of the Lekalo executable. Owns the version matrix (15–18), type policies, deterministic DDL, defaults, sequence/index names and capability answers. It never opens a database connection or executes SQL.

## Go executable

Build from the repository root with Go 1.24 or newer:

```sh
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ./lekalo-postgres .
go test ./...
```

Put the resulting executable on `PATH` as `lekalo-postgres` to use the
default provider selection. An absolute executable path can also be
selected for individual invocations:

```sh
export LEKALO_STORAGE_PROVIDER='{"command":["/absolute/path/lekalo-postgres"]}'
```

The executable uses only the Go standard library. Set `GOOS` and `GOARCH`
with `CGO_ENABLED=0` to build for another platform.

## Git package compatibility

The current Git package manifest points to the checked-in JavaScript
bundle so pinned packages continue to resolve through Git without a build
step. Existing `path:` and Git package sources can still be selected
with `LEKALO_STORAGE_PROVIDER`. The two launchers share the same version
matrix and runtime protocol.

```sh
node scripts/build.mjs --check
LEKALO_POSTGRES_GO_BIN="$PWD/lekalo-postgres" node --test test/provider.test.mjs
```

Use `node scripts/build.mjs` to rebuild the JavaScript bundle after changes
to its sources or the shared matrix.

The host validates its model/projection binding, sends data, confines the process and validates the reply. There is no library linkage to this repository and no embedded SQL fallback. Missing/malformed providers fail closed. The legacy default executable name is `lekalo-postgres`.

Operations: `storage.matrix`, `storage.type`, `storage.ddl`, `storage.table`, `storage.join`, `storage.default`, `storage.sequence`, `storage.index-name`. Each result is `{value:...}` or `{error:{kind:"mapping"|"render",detail:...}}`, echoed in the standard response envelope. Types, defaults, indexes and derived tables are JSON data; integer literals cross as decimal strings to preserve i64 precision. `describe` advertises the operation set. `check` accepts no contract rules: this storage provider does not claim to analyze application source code.

The legacy host still owns model admission, migration orchestration and drift comparison. Their existing calls now use this provider for the extracted DDL primitives; further removal of PostgreSQL-specific migration/drift policy is tracked separately. Do not interpret this extraction as completion of the entire storage plugin API.
