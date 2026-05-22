//go:build !windows

package tflocalexpand

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExpand_InPlaceWriteError covers the in-place write-error branch in
// rewriteAll. It relies on Unix file permissions to make os.WriteFile fail,
// so it's excluded from Windows where 0o400 doesn't reliably block writes.
func TestExpand_InPlaceWriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		// chmod 0o400 doesn't stop root from writing.
		t.Skip("root bypasses file permissions")
	}
	tmp := copyInputToTemp(t, "testdata/basic/input")
	path := filepath.Join(tmp, "main.tf")
	require.NoError(t, os.Chmod(path, 0o400))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	e := NewExpander(tmp)
	err := e.Expand(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write")
}
