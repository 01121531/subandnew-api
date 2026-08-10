package managedinstance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManagedInstanceRemoteHTTPIsCentralizedInConnector(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	forbidden := []string{
		"http.NewRequest(",
		"http.NewRequestWithContext(",
		"http.Client{",
		"http.DefaultClient",
		"http.Get(",
		"http.Post(",
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") || name == "connector.go" {
			continue
		}
		contents, readErr := os.ReadFile(name)
		require.NoError(t, readErr)
		for _, token := range forbidden {
			require.NotContainsf(t, string(contents), token, "%s must send remote HTTP through Connector.DoJSON", name)
		}
	}
}
