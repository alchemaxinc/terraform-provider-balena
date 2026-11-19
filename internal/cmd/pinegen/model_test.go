package main

import (
	"strings"
	"testing"
)

// ENV_VAR_MODEL is a minimal compiled v6 model for application environment variables.
const ENV_VAR_MODEL = `{
  "models": [{
    "apiRoot": "v6",
    "abstractSql": {
      "synonyms": {"application environment variable": "application-has-env var name"},
      "tables": {
        "application": {"idField": "id", "fields": [
          {"fieldName": "id", "dataType": "Serial", "required": true}
        ]},
        "application-has-env var name": {"idField": "id", "writableFields": ["application", "name", "value"], "fields": [
          {"fieldName": "id", "dataType": "Serial", "required": true},
          {"fieldName": "application", "dataType": "ForeignKey", "required": true,
           "references": {"resourceName": "application", "fieldName": "id"}},
          {"fieldName": "name", "dataType": "Short Text", "required": true},
          {"fieldName": "value", "dataType": "Text", "required": true}
        ]}
      }
    }
  }]
}`

func TestGenerateRejectsContractDrift(t *testing.T) {
	tests := []struct {
		name, from, to, wantErr string
	}{
		{"missing alias", "application environment variable", "renamed resource", "missing resource"},
		{"removed field", `"fieldName": "name"`, `"fieldName": "renamed"`, `missing readable field "name"`},
		{"nullable value", `"dataType": "Text", "required": true`, `"dataType": "Text", "required": false`, `"value" no longer matches`},
		{"computed value", `"dataType": "Text"`, `"dataType": "Text", "computed": ["Text", "example"]`, `"value" is missing or not writable`},
		{"missing public write mapping", `, "value"]`, `, "internal_value"]`, `"value" is missing or not writable`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := strings.Replace(ENV_VAR_MODEL, tt.from, tt.to, 1)
			_, err := generate([]byte(data), "v6")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("generate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateUsesRequestedVersion(t *testing.T) {
	data := []byte(strings.ReplaceAll(ENV_VAR_MODEL, `"v6"`, `"v7"`))
	source, err := generate(data, "v7")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), `"/v7/application_environment_variable"`) {
		t.Fatalf("generated client does not target v7:\n%s", source)
	}
	if _, err := generate(data, "v6"); err == nil {
		t.Fatal("generate accepted a model with no v6 API root")
	}
}
