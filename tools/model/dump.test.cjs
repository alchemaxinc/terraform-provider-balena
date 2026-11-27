const { test } = require("node:test");
const assert = require("node:assert");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const dump = path.join(__dirname, "dump.cjs");
const fixture = path.join(__dirname, "testdata", "fixture.d.ts");

function run(dts = fixture) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "model-dump-"));
  const out = path.join(dir, "out.json");
  const report = path.join(dir, "report.md");
  const result = spawnSync(process.execPath, [dump, dts, out, report], { encoding: "utf8" });
  const exists = fs.existsSync(out);
  return {
    status: result.status,
    stderr: result.stderr,
    raw: exists ? fs.readFileSync(out, "utf8") : null,
    model: exists ? JSON.parse(fs.readFileSync(out, "utf8")) : null,
    report: fs.existsSync(report) ? fs.readFileSync(report, "utf8") : "",
  };
}

function field(model, entity, apiName) {
  const e = model.entities.find((x) => x.name === entity);
  assert.ok(e, `entity ${entity} missing`);
  return e.fields.find((f) => f.apiName === apiName);
}

test("entity names prefer a synonym without double underscores", () => {
  const { model } = run();
  const e = model.entities.find((x) => x.name === "user__has__public_key");
  assert.strictEqual(e.terraformName, "user_public_key");
  assert.deepStrictEqual(e.synonyms, ["user_public_key"]);
});

test("field names are lower-cased and reserved names get a _field suffix", () => {
  const { model } = run();
  assert.strictEqual(field(model, "application", "includes__SLA").terraformName, "includes__sla");
  assert.strictEqual(field(model, "application", "provider").terraformName, "provider_field");
});

test("references become integers with their target", () => {
  const { model } = run();
  assert.deepStrictEqual(field(model, "application", "organization"), {
    apiName: "organization",
    terraformName: "organization",
    declaredType: "reference",
    kind: "integer",
    nullable: false,
    enum: [],
    reference: "organization",
    writable: true,
  });
  const f = field(model, "application", "should_be_running__release");
  assert.strictEqual(f.nullable, true);
  assert.strictEqual(f.reference, "release");
});

test("string-literal unions become enums", () => {
  const { model } = run();
  const f = field(model, "application", "is_of__class");
  assert.strictEqual(f.kind, "string");
  assert.strictEqual(f.declaredType, "enum");
  assert.deepStrictEqual(f.enum, ["fleet", "block", "app"]);
});

test("declared SBVR types map to kinds", () => {
  const { model } = run();
  assert.strictEqual(field(model, "application", "created_at").kind, "datetime");
  assert.strictEqual(field(model, "application", "composition").kind, "json");
  assert.strictEqual(field(model, "application", "ratio").kind, "float");
  const hashed = field(model, "application", "secret_hash");
  assert.strictEqual(hashed.kind, "string");
  assert.strictEqual(hashed.declaredType, "Hashed");
  assert.strictEqual(field(model, "application", "logo").kind, "webresource");
});

test("experimental and hand-typed members are skipped and reported", () => {
  const { model, report } = run();
  assert.strictEqual(field(model, "application", "can_x"), undefined);
  assert.strictEqual(field(model, "application", "plain"), undefined);
  const skipped = report.split("## Unknown type names")[0];
  assert.match(skipped, /## Skipped members/);
  assert.match(skipped, /can_x/);
  assert.match(skipped, /plain/);
});

test("unknown Types names fall back to the resolved kind and are reported", () => {
  const { model, report } = run();
  const f = field(model, "application", "weird");
  assert.strictEqual(f.kind, "string");
  assert.strictEqual(f.declaredType, "Fancy");
  const unknown = report.split("## Unknown type names")[1];
  assert.match(unknown, /Fancy/);
});

test("optional reverse navigations are ignored", () => {
  const { model } = run();
  assert.strictEqual(field(model, "application", "owns__device"), undefined);
});

test("writable follows the Write block", () => {
  const { model } = run();
  const role = model.entities.find((x) => x.name === "role");
  assert.ok(role.fields.length > 0);
  assert.ok(role.fields.every((f) => f.writable === false));
  assert.strictEqual(field(model, "application", "organization").writable, true);
  assert.strictEqual(field(model, "application", "composition").writable, false);
});

test("entities are sorted and source is described", () => {
  const { model } = run();
  const names = model.entities.map((e) => e.name);
  assert.deepStrictEqual(names, [...names].sort());
  assert.strictEqual(model.version, 1);
  assert.strictEqual(model.source.package, "balena-sdk");
  assert.strictEqual(model.source.apiVersion, "v7");
});

test("colliding terraform names fail", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "model-collide-"));
  const dts = path.join(dir, "collide.d.ts");
  fs.writeFileSync(
    dts,
    `type Types = { Integer: { Read: number; Write: number } };
interface A { Read: { id: Types['Integer']['Read'] }; Write: {} }
interface B { Read: { id: Types['Integer']['Read'] }; Write: {} }
export default interface $Model { Foo: A; foo: B }
`,
  );
  const result = run(dts);
  assert.strictEqual(result.status, 1);
  assert.match(result.stderr, /collision/);
});

test("output is deterministic", () => {
  assert.strictEqual(run().raw, run().raw);
});
