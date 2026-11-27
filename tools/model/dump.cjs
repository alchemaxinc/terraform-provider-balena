const fs = require("node:fs");
const path = require("node:path");
const ts = require("typescript");

const KINDS = {
  Integer: "integer",
  "Big Integer": "integer",
  Serial: "integer",
  "Short Text": "string",
  Text: "string",
  Hashed: "string",
  "Date Time": "datetime",
  Date: "datetime",
  JSON: "json",
  Real: "float",
  WebResource: "webresource",
  Boolean: "boolean",
};

const RESERVED = new Set([
  "count",
  "depends_on",
  "for_each",
  "lifecycle",
  "provider",
  "providers",
  "connection",
  "provisioner",
]);
const VALID_NAME = /^[a-z][a-z0-9_]*$/;

function fail(message) {
  process.stderr.write(`${message}\n`);
  process.exit(1);
}

function sdkVersion(dtsPath) {
  let dir = path.dirname(path.resolve(dtsPath));
  for (;;) {
    const candidate = path.join(dir, "package.json");
    if (fs.existsSync(candidate)) {
      const pkg = JSON.parse(fs.readFileSync(candidate, "utf8"));
      if (pkg.name === "balena-sdk") return pkg.version;
    }
    const parent = path.dirname(dir);
    if (parent === dir) return "unknown";
    dir = parent;
  }
}

function memberName(member) {
  const n = member.name;
  return ts.isIdentifier(n) || ts.isStringLiteral(n) ? n.text : n.getText();
}

function unionParts(typeNode) {
  return ts.isUnionTypeNode(typeNode) ? typeNode.types : [typeNode];
}

function isNullNode(node) {
  return ts.isLiteralTypeNode(node) && node.literal.kind === ts.SyntaxKind.NullKeyword;
}

function stringLiterals(typeNode) {
  const parts = unionParts(typeNode).filter((p) => !isNullNode(p));
  if (parts.length === 0) return null;
  const values = [];
  for (const p of parts) {
    if (!ts.isLiteralTypeNode(p) || !ts.isStringLiteral(p.literal)) return null;
    values.push(p.literal.text);
  }
  return values;
}

function resolvedKind(checker, typeNode) {
  const parts = unionParts(typeNode).filter((p) => !isNullNode(p));
  const flags = parts.map((p) => checker.getTypeFromTypeNode(p).flags);
  if (flags.every((f) => f & ts.TypeFlags.StringLike)) return "string";
  if (flags.every((f) => f & ts.TypeFlags.NumberLike)) return "float";
  if (flags.every((f) => f & ts.TypeFlags.BooleanLike)) return "boolean";
  return "string";
}

function terraformFieldName(apiName) {
  const lower = apiName.toLowerCase();
  return RESERVED.has(lower) ? `${lower}_field` : lower;
}

function propertyNode(members, name) {
  return members.find((m) => ts.isPropertySignature(m) && memberName(m) === name);
}

function main() {
  const [dts, outPath, reportPath] = process.argv.slice(2);
  if (!dts || !outPath || !reportPath) fail("usage: dump.cjs <v7.d.ts> <out.json> <report.md>");

  const program = ts.createProgram([dts], { noEmit: true, skipLibCheck: true });
  const checker = program.getTypeChecker();
  const source = program.getSourceFile(path.resolve(dts)) || program.getSourceFile(dts);
  if (!source) fail(`cannot read ${dts}`);

  const interfaces = new Map();
  let model;
  ts.forEachChild(source, (node) => {
    if (!ts.isInterfaceDeclaration(node)) return;
    if (node.name.text === "$Model") model = node;
    else interfaces.set(node.name.text, node);
  });
  if (!model) fail("no $Model interface found");

  const entities = new Map();
  for (const member of model.members) {
    if (!ts.isPropertySignature(member) || !member.type || !ts.isTypeReferenceNode(member.type)) continue;
    const iface = member.type.typeName.getText();
    const name = memberName(member);
    const entry = entities.get(iface);
    if (entry) entry.names.push(name);
    else entities.set(iface, { iface, names: [name] });
  }

  const skipped = [];
  const unknown = new Map();
  const result = [];

  for (const { iface, names } of entities.values()) {
    const decl = interfaces.get(iface);
    if (!decl) fail(`interface ${iface} is not declared`);
    const [name, ...synonyms] = names;
    const read = propertyNode(decl.members, "Read");
    const readType = read && read.type;
    const writeNode = propertyNode(decl.members, "Write");
    const writable = new Set();
    if (writeNode && writeNode.type) {
      for (const prop of checker.getTypeFromTypeNode(writeNode.type).getProperties()) writable.add(prop.name);
    }

    const fields = [];
    if (readType && ts.isTypeLiteralNode(readType)) {
      for (const m of readType.members) {
        if (!ts.isPropertySignature(m) || !m.type || m.questionToken) continue;
        const apiName = memberName(m);
        const where = `${name}.${apiName}`;

        if (ts.getJSDocTags(m).some((t) => t.tagName.text === "experimental")) {
          skipped.push(`\`${where}\`: experimental`);
          continue;
        }

        const text = m.type.getText();
        const nullable = unionParts(m.type).some(isNullNode);
        const typesMatch = /Types\['([^']+)'\]/.exec(text.replace(/"/g, "'"));
        const literals = stringLiterals(m.type);
        let declaredType;
        let kind;
        let reference = "";
        let enumValues = [];

        if (typesMatch) {
          declaredType = typesMatch[1];
          kind = KINDS[declaredType];
          if (!kind) {
            kind = resolvedKind(checker, m.type);
            if (!unknown.has(declaredType)) unknown.set(declaredType, []);
            unknown.get(declaredType).push(where);
          }
        } else if (text.includes("__id")) {
          declaredType = "reference";
          kind = "integer";
          const target = /__id:\s*(\w+)\['Read'\]\['id'\]/.exec(text.replace(/"/g, "'"));
          const targetEntity = target && entities.get(target[1]);
          reference = targetEntity ? targetEntity.names[0] : "";
        } else if (literals) {
          declaredType = "enum";
          kind = "string";
          enumValues = literals;
        } else {
          skipped.push(`\`${where}\`: hand-typed \`${text}\``);
          continue;
        }

        fields.push({
          apiName,
          terraformName: terraformFieldName(apiName),
          declaredType,
          kind,
          nullable,
          enum: enumValues,
          reference,
          writable: writable.has(apiName),
        });
      }
    }

    const seen = new Map();
    for (const f of fields) {
      if (!VALID_NAME.test(f.terraformName)) fail(`invalid name ${f.terraformName} in ${name}`);
      if (seen.has(f.terraformName)) {
        fail(`name collision in ${name}: ${seen.get(f.terraformName)} and ${f.apiName} -> ${f.terraformName}`);
      }
      seen.set(f.terraformName, f.apiName);
    }

    const terraformName = (names.find((n) => !n.includes("__")) || name).toLowerCase();
    result.push({ name, synonyms, terraformName, fields });
  }

  const seenEntities = new Map();
  for (const e of result) {
    if (!VALID_NAME.test(e.terraformName)) fail(`invalid name ${e.terraformName} for entity ${e.name}`);
    if (seenEntities.has(e.terraformName)) {
      fail(`name collision: ${seenEntities.get(e.terraformName)} and ${e.name} -> ${e.terraformName}`);
    }
    seenEntities.set(e.terraformName, e.name);
  }
  result.sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));

  const output = {
    version: 1,
    source: { package: "balena-sdk", version: sdkVersion(dts), apiVersion: "v7" },
    entities: result,
  };
  fs.writeFileSync(outPath, `${JSON.stringify(output, null, 2)}\n`);

  const sections = [];
  if (skipped.length) sections.push(`## Skipped members\n\n${skipped.map((s) => `- ${s}`).join("\n")}\n`);
  if (unknown.size) {
    const lines = [...unknown].map(([type, uses]) => `- \`${type}\`: ${uses.map((u) => `\`${u}\``).join(", ")}`);
    sections.push(`## Unknown type names\n\n${lines.join("\n")}\n`);
  }
  fs.writeFileSync(reportPath, sections.join("\n"));
}

main();
