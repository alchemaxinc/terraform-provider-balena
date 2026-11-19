// Command pinegen generates the provider's Go bindings from Balena's compiled Pine model.
package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var imagePattern = regexp.MustCompile(`^docker\.io/balena/open-balena-api:v[0-9]+\.[0-9]+\.[0-9]+@sha256:[0-9a-f]{64}$`)

func main() {
	var check bool
	switch {
	case len(os.Args) == 1:
	case len(os.Args) == 2 && os.Args[1] == "-check":
		check = true
	default:
		slog.Error("usage: pinegen [-check]")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	err := run(ctx, check)
	cancel()
	if err != nil {
		slog.With("error", err).Error("failed to generate Pine client")
		os.Exit(1)
	}
}

func run(ctx context.Context, check bool) error {
	const (
		IMAGE_FILE  = "schema/pine-image"
		OUTPUT_FILE = "internal/balena/environment_variable.gen.go"
		API_VERSION = "v6"
	)
	pin, err := os.ReadFile(IMAGE_FILE)
	if err != nil {
		return err
	}
	image := strings.TrimSpace(string(pin))
	if !imagePattern.MatchString(image) {
		return fmt.Errorf("%s must pin a Balena API release image by digest", IMAGE_FILE)
	}

	data, err := materialize(ctx, image)
	if err != nil {
		return err
	}
	generated, err := generate(data, API_VERSION)
	if err != nil {
		return err
	}
	if !check {
		return os.WriteFile(OUTPUT_FILE, generated, 0o644)
	}
	current, err := os.ReadFile(OUTPUT_FILE)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, generated) {
		return fmt.Errorf("%s is stale; run make generate-pine", OUTPUT_FILE)
	}
	return nil
}

func materialize(ctx context.Context, image string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker",
		"run", "--rm", "-i", "--network", "none", "--read-only", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges", "--user", "65534:65534",
		"--tmpfs", "/tmp:rw,noexec,nosuid,nodev", "--workdir", "/tmp",
		"--entrypoint", "node", image, "--input-type=module", "-")
	cmd.Stdin = strings.NewReader(COMPILER_INPUT)
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("materializing Balena's Pine model: %w", err)
	}
	return data, nil
}

// COMPILER_INPUT loads Balena's public models and validates their Pine translation chain.
const COMPILER_INPUT = `
import { readdir } from 'node:fs/promises';
import { model } from '/usr/src/app/src/balena.ts';
import { translateAbstractSqlModel } from '/usr/src/app/node_modules/@balena/pinejs/out/sbvr-api/translations.js';

const directory = '/usr/src/app/src/translations';
const versions = (await readdir(directory, { withFileTypes: true }))
	.filter(entry => entry.isDirectory() && /^v\d+$/.test(entry.name))
	.map(entry => entry.name).sort();
const models = [model];
const compiled = new Map([[model.apiRoot, model.abstractSql]]);
for (const version of versions) {
	const exported = await import(directory + '/' + version + '/' + version + '.ts');
	const abstractSql = exported[version + 'AbstractSqlModel'];
	const translations = exported['get' + version.toUpperCase() + 'Translations']();
	models.push({ apiRoot: version, abstractSql, translations, translateTo: exported.toVersion });
}
const publicModels = structuredClone(models);
const publicByRoot = new Map(publicModels.map(entry => [entry.apiRoot, entry]));
while (compiled.size < models.length) {
	const next = models.find(({ apiRoot, translateTo }) => !compiled.has(apiRoot) && compiled.has(translateTo));
	if (next == null) {
		throw new Error('Unresolved Pine translation chain');
	}
	const { apiRoot, abstractSql, translateTo, translations } = next;
	translateAbstractSqlModel(abstractSql, compiled.get(translateTo), apiRoot, translateTo, translations);
	compiled.set(apiRoot, abstractSql);
}
function writable(root, resource, name) {
	const entry = publicByRoot.get(root);
	const table = entry.abstractSql.tables[resource];
	const field = table?.fields.find(field => field.fieldName === name);
	if (field == null || field.computed != null) {
		return false;
	}
	if (entry.translateTo == null) {
		return (table.modifyFields ?? (table.definition == null ? table.fields : []))
			.some(field => field.fieldName === name && field.computed == null);
	}
	const mapping = entry.translations[resource] ?? {};
	const target = Object.hasOwn(mapping, name) ? mapping[name] : name;
	return mapping.abstractSql == null && typeof target === 'string' &&
		writable(entry.translateTo, mapping.$toResource ?? resource, target);
}
for (const { apiRoot, abstractSql } of publicModels) {
	for (const [resource, table] of Object.entries(abstractSql.tables)) {
		table.writableFields = table.fields.filter(field => writable(apiRoot, resource, field.fieldName))
			.map(field => field.fieldName);
	}
}
process.stdout.write(JSON.stringify({ models: publicModels }));
`
