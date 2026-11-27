package internal

import (
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/require"
)

const sampleTOML = `# top comment
pruning = "default"
min-retain-blocks = 9750

[api]
enable = false

[performance]
# Write the stores in parallel.
parallel-commit = true
execution-caches = true

[[array]]
x = 1
`

func decode(t *testing.T, bz []byte) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, toml.Unmarshal(bz, &m), "output is not valid TOML:\n%s", bz)
	return m
}

func TestSetTOMLKeys(t *testing.T) {
	t.Run("rewrites and inserts keys in a table", func(t *testing.T) {
		out := SetTOMLKeys([]byte(sampleTOML), "performance", map[string]string{
			"parallel-commit":      "false",
			"deferred-persistence": `"off"`,
		})
		m := decode(t, out)
		perf := m["performance"].(map[string]any)
		require.Equal(t, false, perf["parallel-commit"])
		require.Equal(t, true, perf["execution-caches"])
		require.Equal(t, "off", perf["deferred-persistence"])
		require.Equal(t, false, m["api"].(map[string]any)["enable"])
		require.Contains(t, string(out), "# Write the stores in parallel.")
	})

	t.Run("top-level keys stay out of tables", func(t *testing.T) {
		out := SetTOMLKeys([]byte(sampleTOML), "", map[string]string{
			"pruning":                `"everything"`,
			"parallel_commit_verify": "false",
		})
		m := decode(t, out)
		require.Equal(t, "everything", m["pruning"])
		require.Equal(t, false, m["parallel_commit_verify"])
		require.Equal(t, int64(9750), m["min-retain-blocks"])
		_, inAPI := m["api"].(map[string]any)["parallel_commit_verify"]
		require.False(t, inAPI)
	})

	t.Run("appends a missing table", func(t *testing.T) {
		out := SetTOMLKeys([]byte(sampleTOML), "blocksync", map[string]string{"pipeline": "false", "max_requesters": "40"})
		bs := decode(t, out)["blocksync"].(map[string]any)
		require.Equal(t, false, bs["pipeline"])
		require.Equal(t, int64(40), bs["max_requesters"])
	})

	t.Run("a key of the same name in another table is not touched", func(t *testing.T) {
		out := SetTOMLKeys([]byte(sampleTOML), "api", map[string]string{"enable": "true"})
		m := decode(t, out)
		require.Equal(t, true, m["api"].(map[string]any)["enable"])
		require.Equal(t, int64(1), m["array"].([]map[string]any)[0]["x"])
	})

	t.Run("is idempotent", func(t *testing.T) {
		v := map[string]string{"parallel-commit": "false", "async-pruning": "false"}
		once := SetTOMLKeys([]byte(sampleTOML), "performance", v)
		require.Equal(t, string(once), string(SetTOMLKeys(once, "performance", v)))
	})
}
