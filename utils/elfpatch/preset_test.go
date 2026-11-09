package elfpatch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goccy/go-yaml"
)

const presetDir = "../../patch-presets/cfw-A8640EU-common"

// TestPresetsParse keeps the shipped presets in step with the schema and, when
// the stubs have been built, checks that each referenced entry links.
func TestPresetsParse(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(presetDir, "*.elf.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no presets found: %v", err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var ps PatchSet
		if err := yaml.UnmarshalWithOptions(data, &ps, yaml.DisallowUnknownField()); err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		if err := ps.Validate(); err != nil {
			t.Errorf("%s: %v", file, err)
		}
		for _, e := range ps.Edits {
			path, ok := ps.Objects[e.Object]
			if !ok {
				if e.Object != "" {
					t.Errorf("%s: edit %q uses undeclared object %q", file, e.Name, e.Object)
				}
				continue
			}
			obj, err := os.ReadFile(filepath.Join(presetDir, path))
			if err != nil {
				continue // stubs not built
			}
			if _, err := LoadObject(obj, e.Entry); err != nil {
				t.Errorf("%s: edit %q: %v", file, e.Name, err)
			}
		}
	}
}
