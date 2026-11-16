# Schema pipeline

The provider's resources and data sources are generated from balena's own models. Nothing per table is hand-written
except named hooks for genuinely bespoke behaviour.

```
schema/sources.lock.json   pinned tags, commit SHAs and sha256 of every upstream file   (make fetch, network)
schema/upstream/**         the vendored upstream files (schema/balena.sbvr too)
schema/overrides.yaml      the only hand-edited input
        |
        v  make generate   (go run ./cmd/balena-schema generate: pure Go, offline, deterministic)
schema/model.json                       the IR: every table of every locked root, classified
schema/surface.lock.json                the Terraform-visible surface; its attribute names are sticky
schema/coverage.md                      gap report: every table generated / handwritten / excluded / unscoped
internal/provider/zz_generated_specs.go the specs one generic resource and one generic data source run on
examples/{resources,data-sources}/balena_*/   one example per type (docs: make docs)
```

## Sources

| Source                                                               | Used for                                                                                                  |
| -------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| balena-sdk `src/types/v<N>.ts`                                       | shape of balena-cloud's versioned root: tables, columns, types, nullability, references, user writability |
| open-balena-api `src/balena-model.ts`                                | shape of the unversioned `/resin` root (tables balena-cloud does not version, such as profiles)           |
| open-balena-api `src/balena.sbvr`, `src/translations/v<N>/v<N>.sbvr` | lengths, enums, uniqueness, natural keys, notes; cross-checked against the TypeScript models              |
| open-balena-api `src/lib/auth.ts`                                    | user permissions of tables only the unversioned model has                                                 |
| `GET https://api.balena-cloud.com/config/vars`                       | variable-name rules                                                                                       |

The SBVR is not the whole model: open-balena-api renames columns and adds computed fields in code. Generation fails on
any difference between an SBVR file and the TypeScript model of the same root that `internal/gen/known.go` does not
explain (four renames, ten computed fields, one view, one type change, and the internal tables and secrets the cloud
typings leave out). Columns only balena-cloud has are recorded as `cloud-only`.

## Rules

- **Roots.** Roots are data in the lock. `make fetch` discovers every `v<N>.ts` balena-sdk ships; the lock's
  `default_root` is the root tables use when it has them; other tables use `/resin` and are marked preview.
- **Classification.** Payment and billing tables (name words `plan`, `subscription`, `credit`, `discount`, `billing`,
  `invoice`, `addon`, `feature`, `support`), tables with no column besides their id, and tables only the unversioned
  model has that open-balena-api grants users no access to are excluded. Every other table gets a data source, and a
  resource when users can write a column (for unversioned-only tables: when `auth.ts` lets users create rows). Tables
  outside `scope` are reported as unscoped with the classification these rules give them.
- **Columns.** Every column is exposed except payment columns, `modified_at` (changes on every write and is not readable
  everywhere on balena-cloud), references to tables with no data (`actor`), and file uploads.
- **Modes.** Columns users cannot write are computed. JSON columns are computed. Booleans, unique server-generated
  identifiers (`slug`, `uuid`, `handle`) and nullable references are optional and computed. Other nullable columns are
  optional; the rest are required. A required reference or a role of a link table's natural key forces replacement.
- **Validators.** SBVR length bounds and TypeScript enums on writable string columns, and `/config/vars` name rules
  where `overrides.yaml` applies them.
- **Type names.** `balena_` plus the API resource name, with the `__` of link-table names collapsed: no abbreviations.
- **Attribute names.** The leading verb of a column is stripped (`is_for__device_type` becomes `device_type`) unless
  that leaves a bare `date`. References get `_id`, except references exposed by a column of the referenced row
  (`device_type` is a slug). When stripped names collide, the one column without a verb keeps the stripped name and the
  others keep their full name with `__` collapsed (`is_pinned_on_release_id`); with no such column all keep their full
  name. Names already in `surface.lock.json` never change: a new column whose name would collide keeps its full name.
  Terraform-reserved names fail generation.

## overrides.yaml

Every entry needs a `reason`. Unknown tables or columns, overrides equal to the rule result, rules that match nothing,
hooks that are not declared, not used, or have no Go function in `internal/provider`, and hook `uses` that are not IR
columns fail generation.

- `scope`: the tables the provider manages.
- `rules.sensitive`, `rules.var_names`: column rules by table suffix or table list.
- `hooks`: named Go hooks with the API columns they use. `TestHooksUseOnlyDeclaredColumns` runs every hook against a
  fake API and fails when it touches an undeclared column; `make contract` checks the declared columns live.
- `tables.<table>`: `resource: false`, `resource_fields` (resource subset), `import_by` (unique column),
  `hooks.create` / `hooks.delete`, and per column `mode`, `force_new`, `sensitive`, `name`, `reference_by` (expose a
  reference by a unique column of the referenced row, or by any column with a `resolver` hook).

## Gates

All offline gates are deterministic and fail closed:

- `make check-generate`: lock hashes, no unpinned file under `schema/upstream`, cross-check, gap check, override
  validation, and no difference between the committed and the generated files (examples included).
- `make surface-diff BASE=<old surface.lock.json> TITLE="<pr title>"`: classifies the surface change (`feat!` for
  anything that can break a configuration or state, `feat` for additions, `fix` for loosened validation) and fails when
  the title's conventional commit type is smaller.
- `make contract` (needs `BALENA_API_TOKEN`): read-only `GET /<root>/<table>?$top=1&$select=<columns>` for every spec
  and hook. Pine.js answers 401 to an unknown column, so a 200 proves every column.

## Widening the scope

Add table names to `scope`, run `make generate`, read the new rows of `schema/coverage.md`, add overrides only where
the rules give something unacceptable, run `make contract`, then `make docs`. The surface diff is `feat`.

## A new API root (v8)

1. `make fetch ARGS="-latest"` vendors `src/types/v8.ts` (and `v8.sbvr` when open-balena-api has it) and adds root
   `v8` to the lock without changing `default_root`.
2. `go run ./cmd/balena-schema ir -root v8` builds the IR for v8; `go run ./cmd/balena-schema surface-diff -old-root
v7 -new-root v8` shows what promoting it would change (names stay sticky); `go run ./cmd/balena-schema contract
-root v8` checks it live.
3. Promote with `go run ./cmd/balena-schema fetch -default-root v8` and `make generate`.
