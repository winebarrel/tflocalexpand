package tflocalexpand

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpand_Golden(t *testing.T) {
	cases := []string{"basic", "chained", "interp", "nested", "unresolved"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			tmp := copyInputToTemp(t, filepath.Join("testdata", name, "input"))
			e := NewExpander(tmp)
			require.NoError(t, e.Expand(true))
			compareDir(t, tmp, filepath.Join("testdata", name, "expected"))
		})
	}
}

func TestExpand_Cycle(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/cycle/input")
	e := NewExpander(tmp)
	err := e.Expand(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular")
}

func copyInputToTemp(t *testing.T, srcDir string) string {
	t.Helper()
	tmp := t.TempDir()
	entries, err := os.ReadDir(srcDir)
	require.NoError(t, err)
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, ent.Name()))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(tmp, ent.Name()), data, 0o644))
	}
	return tmp
}

func compareDir(t *testing.T, gotDir, wantDir string) {
	t.Helper()
	entries, err := os.ReadDir(wantDir)
	require.NoError(t, err)
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		got, err := os.ReadFile(filepath.Join(gotDir, ent.Name()))
		require.NoError(t, err)
		want, err := os.ReadFile(filepath.Join(wantDir, ent.Name()))
		require.NoError(t, err)
		assert.Equal(t, string(want), string(got), "file %s", ent.Name())
	}
}
