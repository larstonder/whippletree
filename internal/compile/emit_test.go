package compile_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/larstonder/adapter-sdk/internal/compile"
	"github.com/larstonder/adapter-sdk/internal/target"
)

// setupBundle copies the kb-shaped fixture into a fresh temp bundle dir
// as plugin.json, returning the bundle dir.
func setupBundle(t *testing.T) string {
	t.Helper()

	src, err := os.ReadFile("testdata/bundle-plugin.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), src, 0o644); err != nil {
		t.Fatalf("write bundle plugin.json: %v", err)
	}
	return dir
}

func loadGolden(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "golden", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return b
}

func TestBuild_EmitsPerTargetVariants(t *testing.T) {
	bundleDir := setupBundle(t)

	targets, err := target.LoadDir("../../targets")
	if err != nil {
		t.Fatalf("target.LoadDir: %v", err)
	}

	if _, err := compile.Build(bundleDir, targets); err != nil {
		t.Fatalf("compile.Build: %v", err)
	}

	// 1 & 2: hooks files byte-equal their goldens.
	for _, tc := range []struct {
		targetName string
		golden     string
	}{
		{"codex", "codex.json"},
		{"claude-code", "claude-code.json"},
	} {
		got, err := os.ReadFile(filepath.Join(bundleDir, "hooks", tc.targetName+".json"))
		if err != nil {
			t.Fatalf("read hooks/%s.json: %v", tc.targetName, err)
		}
		want := loadGolden(t, tc.golden)
		if string(got) != string(want) {
			t.Errorf("hooks/%s.json mismatch\n--- got ---\n%s\n--- want ---\n%s", tc.targetName, got, want)
		}
	}

	// 3: manifest hooks keys point at the right hooks file per target.
	for _, tc := range []struct {
		manifestDir string
		wantHooks   string
	}{
		{".claude-plugin", "./hooks/claude-code.json"},
		{".codex-plugin", "./hooks/codex.json"},
	} {
		raw, err := os.ReadFile(filepath.Join(bundleDir, tc.manifestDir, "plugin.json"))
		if err != nil {
			t.Fatalf("read %s/plugin.json: %v", tc.manifestDir, err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal %s/plugin.json: %v", tc.manifestDir, err)
		}
		if got := m["hooks"]; got != tc.wantHooks {
			t.Errorf("%s/plugin.json[\"hooks\"] = %v, want %v", tc.manifestDir, got, tc.wantHooks)
		}
		if m["name"] != "kb-shaped" {
			t.Errorf("%s/plugin.json[\"name\"] = %v, want carried-over %q", tc.manifestDir, m["name"], "kb-shaped")
		}
	}

	// 4: hooks/hooks.json must never exist.
	if _, err := os.Stat(filepath.Join(bundleDir, "hooks", "hooks.json")); err == nil {
		t.Error("hooks/hooks.json exists, want it to never be emitted")
	} else if !os.IsNotExist(err) {
		t.Errorf("stat hooks/hooks.json: unexpected error %v", err)
	}

	// 5: codex hooks file top level has ONLY the "hooks" key.
	codexRaw, err := os.ReadFile(filepath.Join(bundleDir, "hooks", "codex.json"))
	if err != nil {
		t.Fatalf("read hooks/codex.json: %v", err)
	}
	var codexTop map[string]any
	if err := json.Unmarshal(codexRaw, &codexTop); err != nil {
		t.Fatalf("unmarshal hooks/codex.json: %v", err)
	}
	if len(codexTop) != 1 {
		t.Fatalf("codex hooks top-level keys = %v, want exactly {\"hooks\"}", keysOf(codexTop))
	}
	if _, ok := codexTop["hooks"]; !ok {
		t.Fatalf("codex hooks top-level keys = %v, want exactly {\"hooks\"}", keysOf(codexTop))
	}

	// 6: vendored .adapter-sdk files exist.
	for _, p := range []string{
		filepath.Join(".adapter-sdk", "contract.json"),
		filepath.Join(".adapter-sdk", "targets", "codex.yaml"),
	} {
		if _, err := os.Stat(filepath.Join(bundleDir, p)); err != nil {
			t.Errorf("expected vendored file %s to exist: %v", p, err)
		}
	}
}

func keysOf(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
