# API model and code generation

The provider is being generated from the Balena API model, so that it follows the API with as little handwritten code as possible.

## Pipeline

```text
VERSION ─► extract.sh ─► raw config ─► schemagen normalize ─► api/v6.json ─► schemagen generate ─► internal/generated/zz_generated.go
                         (not kept)                            api/provenance.json                    │
                                                                                                       ▼
                                                                                       internal/engine (generic resources)
```

1. **`VERSION`** pins an [open-balena-api](https://github.com/balena-io/open-balena-api) release.
2. **`extract.sh`** runs upstream's own model export (`GENERATE_CONFIG`) for that release in a pinned Node container. Dependency installation has network access with install scripts disabled. The export has no network, runs as a non-root user with no capabilities and read-only sources, and runs twice: the outputs must be byte-identical. The raw config is never committed.
3. **`schemagen normalize`** reduces the export to `api/v6.json`: every resource `/v6` serves, with wire names, types, foreign keys, required flags, server defaults, enums, and whether writes can reach each field. Upstream's TypeScript changes to the model and its `v6` → `v7` → base translations are already applied in the export. The normalizer follows each field down the translation chain to decide whether it is read-only.
4. **`schemagen generate`** maps the spec to Terraform descriptors in `internal/generated/zz_generated.go`.
5. **`internal/engine`** implements every descriptor as a resource and a data source.

`api/provenance.json` records the tag, commit, lockfile hash, PineJS version, Node image digest and raw config hash of the extraction.

## Commands

```shell
make api-spec   # extract, normalize and regenerate for schema/VERSION (needs Docker)
make generate   # regenerate code from the committed spec
```

## Files you must not edit by hand

- `api/v6.json`, `api/provenance.json`: written by `make api-spec`.
- `internal/generated/zz_generated.go`: written by `make generate`.

`go test ./...` fails when the generated code is stale, when the spec is not in canonical form, or when the provenance tag differs from `VERSION`.

## Mapping rules

The generator applies only PineJS and SBVR semantics; no Balena resource name appears in `internal/schemagen` or `internal/engine`.

| Spec                          | Resource attribute                     | Data source attribute |
| ----------------------------- | -------------------------------------- | --------------------- |
| read-only (computed upstream) | Computed                               | Computed              |
| required, no server default   | Required                               | Optional filter       |
| server default                | Optional + Computed                    | Optional filter       |
| nullable                      | Optional; removing it clears the value | Optional filter       |
| `Hashed`                      | Optional, Sensitive, never read back   | omitted               |
| foreign key                   | numeric id                             | numeric id filter     |
| `Date Time`, `JSON`           | string                                 | Computed              |

Behavior the model does not contain, such as values that upstream hooks fill in, permissions, sensitive values and replacement rules, belongs in a separate override layer, not in the generator.

## Weekly update

`.github/workflows/update-api-spec.yml` checks for a new upstream release every Monday. It extracts in a job without secrets, regenerates and tests with trusted code from `main`, and classifies the change of the generated surface with `schemagen diff`:

| Change                            | Pull request                          | Merge                  |
| --------------------------------- | ------------------------------------- | ---------------------- |
| none (only `VERSION`, provenance) | `chore: bump schema VERSION to …`     | automatic              |
| additive                          | `feat: update balena API model to …`  | automatic              |
| breaking                          | `feat!: update balena API model to …` | waits for a maintainer |
| extraction or regeneration fails  | Copilot task on the update branch     | review                 |

While the generated resources are not yet registered in the provider (`SURFACE_RELEASED: "false"`), surface changes use `build:`, which runs the acceptance tests without cutting a release.
