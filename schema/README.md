# Schema pipeline

The provider's resources and data sources are generated from balena's own models. Nothing per table is hand-written
except named hooks for genuinely bespoke behaviour.

```
schema/sources.lock.json   pinned tags, commit SHAs and sha256 of every upstream file   (make fetch, network)
schema/upstream/**         the vendored upstream files: the only copy of every upstream input
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
- **Attribute names**, in order of precedence:

  1. a table override's `name`;
  2. the `names` map in `overrides.yaml`: a role name per API field, in every table (`should_be_running__release` is
     `target_release`, `is_pinned_on__release` is `pinned_release`);
  3. the name in `surface.lock.json`: names are sticky, so neither a rule change nor a new upstream column renames an
     existing attribute;
  4. the rules: strip the leading verb (`is_for__device_type` is `device_type`), or drop a generic remainder instead
     (`is_locked_until__date` is `is_locked_until`). When stripped names collide, the one column without a verb keeps
     the stripped name and the others keep their full name with `__` collapsed; `coverage.md` lists them, and a `names`
     entry fixes any that read badly. Rule names are lowercased, and a name Terraform reserves gets `_name`
     (`provider` is `provider_name`).

  References get `_id`, except references exposed by a column of the referenced row, which stay bare (`device_type` is
  a slug; `references` and `reference_by` below). Booleans, the API's own names (`app_name`, `class`) and type names
  are kept as they are. Every name must match `^[a-z_][a-z0-9_]*$`, or generation fails.

## overrides.yaml

Every entry needs a `reason`. Unknown tables or columns, overrides equal to the rule result, rules that match nothing,
hooks that are not declared, not used, or have no Go function in `internal/provider`, and hook `uses` that are not IR
columns fail generation.

- `scope`: the tables the provider manages.
- `rules.sensitive`, `rules.var_names`: column rules by table suffix or table list. An in-scope column whose API name
  looks like a secret (`token`, `secret`, `password`/`passwd`, `credential`, `private_key`, `api_key`/`apikey`,
  `auth_key`; not `public_key`) and that is not `Hashed` fails generation until a `rules.sensitive` entry or its own
  `sensitive: true` or `false` decides it.
- `names`: role names by API field, applied in every table, unscoped ones included. An entry no column of any locked
  root has, or one that only repeats what the rules give, fails generation.
- `references`: expose every reference to a table by one of its unique columns (`device_type` by `slug`).
- `hooks`: named Go hooks with the API columns they use. `TestHooksUseOnlyDeclaredColumns` runs every hook against a
  fake API and fails when it touches an undeclared column; `make contract` checks the declared columns live.
- `tables.<table>`: `resource: false`, `resource_fields` (resource subset), `import_by` (unique column),
  `hooks.create` / `hooks.delete`, and per column `mode`, `force_new`, `sensitive`, `name`, `reference_by` (expose one
  reference by a column of the referenced row, unique or resolved by a `resolver` hook). Like a `names` entry, a column
  entry may be for a column only another locked root's table has (`device.api_secret` exists only in `/resin`).

## Gates

All offline gates are deterministic and fail closed:

- `make check-generate`: lock hashes, no unpinned file under `schema/upstream`, cross-check, gap check, override
  validation, and no difference between the committed and the generated files (examples included).
- `make surface-diff BASE=<old surface.lock.json> TITLE="<pr title>"`: classifies the surface change (`feat!` for
  anything that can break a configuration or state, `feat` for additions, `fix` for loosened validation) and fails when
  the title's conventional commit type is smaller.
- `make contract` (needs `BALENA_API_TOKEN`): read-only `GET /<root>/<table>?$top=1&$select=<columns>` for every spec
  and hook. Pine.js answers 401 to an unknown column, so a 200 proves every column.

## Renaming on purpose

Generation never renames an attribute by itself. To rename:

- one API field everywhere: add a `names` entry and run `make generate`;
- after changing a naming rule: run `make generate ARGS=-fresh-names`, which names every attribute by the current
  rules and `names` map instead of the lock (`coverage.md` lists the sticky names a fresh run would change).

Commit the regenerated `surface.lock.json` with the rest. Every renamed attribute shows in the surface diff as removed
and added, so the pull request needs a `feat!` title (the surface compatibility check enforces it). The weekly workflow
never renames: it does not pass `-fresh-names`, and the agent it starts may only touch `overrides.yaml` and hooks in a
draft pull request a human reviews.

## Widening the scope

Add table names to `scope`, run `make generate`, read the new rows of `schema/coverage.md`, add overrides only where
the rules give something unacceptable, run `make contract`, then `make docs`. The surface diff is `feat`.

## Weekly regeneration

`.github/workflows/check-schema.yml` runs every Monday (and on dispatch from `main`):

1. Resolve the newest open-balena-api and balena-sdk tags (strictly validated) and pin them with `fetch`.
2. `promote-root -apply`: promote a newer API root when it is ready (below).
3. `make generate`, `make docs`.
4. Gates: `make check-generate`; `go build`, `go vet` and `make test-unit`; `make contract` against the live default
   root.
5. Nothing changed: the run ends quietly. Changed and every gate passed: a pull request through the GitHub App, set to
   auto-merge once CI passes. Its type comes from the surface diff against the committed `surface.lock.json`: `feat!`,
   `feat` or `fix` as classified, otherwise `fix` when generated provider files changed and `chore` when only `schema/`
   did.
6. A gate failed: nothing is merged, the run fails, the regenerated state is pushed to a branch, and a Copilot task is
   started with the failing gates' output. The task may only change `schema/overrides.yaml` and the hooks in
   `internal/provider/hooks.go` (`.github/prompts/schema-update.md`); its pull request is a draft for a human.

Privileges are split by job. `regenerate` runs steps 1-4, which build and run repository and upstream code, with only
the read-only `development` token and no write access; it uploads the generated paths as an artifact. `publish` holds
the GitHub App key (environment `housekeeping`, `main` only) and runs only git, gh and the helpers of `main`: it copies
the allowlisted generated paths (`.github/scripts/apply-artifact.sh`) and pushes. The Copilot PAT stays in
`copilot-agent`. `update-provider-version-examples.yml` is split the same way.

## A new API root (v8)

`fetch` adds every root balena-sdk ships (`src/types/v<N>.ts`, plus `v<N>.sbvr` when open-balena-api has one) to the
lock without changing `default_root`. Generation parses and cross-checks only the roots it draws tables from (the
default root and `/resin`), so a new root that does not parse or disagrees with its SBVR never blocks it; it blocks only
its own promotion. The hashes of every locked file are still verified.

`promote-root` (`make promote-root`, needs `BALENA_API_TOKEN`) takes the newest root newer than `default_root`:

- It builds the IR for the new root, with every in-scope table the root lacks served by `/resin` (listed in the plan),
  and runs the live contract against it. Both pass: the plan is ready, and `-apply` flips `default_root` and
  regenerates; the surface diff gives the pull request type.
- The new root does not cross-check, does not generate, or fails its contract: it is not promoted, the current root
  stays, and the weekly run says so in its summary and a warning but carries on.

When the current root breaks (upstream retires it or its contract fails), a newer root that passes is promoted. When
`fetch` no longer finds the default root upstream, the newest root becomes the default and the contract gate decides.
Otherwise the run fails closed: the contract or check gate fails, nothing merges, and the agent task reports it.

By hand: `go run ./cmd/balena-schema ir -root v8`, `surface-diff -old-root v7 -new-root v8`, `contract -root v8`, and
`promote-root -root v8 [-apply]`.
