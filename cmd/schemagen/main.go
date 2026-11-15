// Command schemagen maintains the provider's API spec and generated code.
//
//	schemagen placeholder-env <compose.yml> <out.env>
//	schemagen normalize <raw-config.json> <spec.json>
//	schemagen generate <spec.json> <out.go>
//	schemagen check <spec.json> <out.go>
//	schemagen diff <old-spec.json> <new-spec.json>
//
// diff prints a JSON object with the change kind (none, additive, breaking),
// the change lists and a Markdown summary of the generated surface change.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/alchemaxinc/terraform-provider-balena/internal/schemagen"
)

// API_ROOT is the API root the provider talks to.
const API_ROOT = "v6"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: schemagen placeholder-env|normalize|generate|check|diff <in> <out>")
	}
	cmd, in, out := args[0], args[1], args[2]
	switch cmd {
	case "placeholder-env":
		compose, err := readLimited(in)
		if err != nil {
			return err
		}
		env, err := schemagen.PlaceholderEnv(compose)
		if err != nil {
			return err
		}
		return os.WriteFile(out, env, 0o644)

	case "normalize":
		raw, err := readLimited(in)
		if err != nil {
			return err
		}
		spec, err := schemagen.Normalize(raw, API_ROOT)
		if err != nil {
			return err
		}
		data, err := schemagen.MarshalSpec(spec)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)

	case "generate", "check":
		spec, err := schemagen.LoadSpec(in)
		if err != nil {
			return err
		}
		src, err := schemagen.Generate(spec)
		if err != nil {
			return err
		}
		if cmd == "generate" {
			return os.WriteFile(out, src, 0o644)
		}
		current, err := os.ReadFile(out)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, src) {
			return fmt.Errorf("%s is stale: run make generate", out)
		}
		return nil

	case "diff":
		oldSpec, err := schemagen.LoadSpec(in)
		if err != nil {
			return err
		}
		newSpec, err := schemagen.LoadSpec(out)
		if err != nil {
			return err
		}
		d, err := schemagen.DiffSurfaces(oldSpec, newSpec)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			*schemagen.SurfaceDiff
			Markdown string `json:"markdown"`
		}{d, d.Markdown()})
	}
	return fmt.Errorf("unknown command %q", cmd)
}

// readLimited reads a file of at most MAX_RAW_CONFIG_BYTES.
func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, schemagen.MAX_RAW_CONFIG_BYTES+1))
	if err != nil {
		return nil, err
	}
	if len(data) > schemagen.MAX_RAW_CONFIG_BYTES {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, schemagen.MAX_RAW_CONFIG_BYTES)
	}
	return data, nil
}
