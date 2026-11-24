package internal

import (
	"bytes"
	"slices"
	"strings"
)

// SetTOMLKeys sets keys in one table of a TOML file and leaves every other
// line untouched. The typed config structs of this module drop keys they do
// not know, so this is how a test sets options of a newer binary.
//
// section "" is the top-level table. values maps a key to its TOML literal,
// e.g. `true`, `"off"` or `40`. A key already present in the table is
// rewritten in place; a missing key is inserted after the table header, and a
// missing table is appended. Dotted or quoted keys and inline tables are not
// supported.
func SetTOMLKeys(content []byte, section string, values map[string]string) []byte {
	lines := strings.Split(string(content), "\n")

	// The table runs from its header (or the file start) to the next header.
	start, end := 0, len(lines)
	found := section == ""
	for i, line := range lines {
		name, isHeader := tableName(line)
		if !isHeader {
			continue
		}
		if found {
			end = i
			break
		}
		if name == section {
			found = true
			start = i + 1
		}
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	if !found {
		var b bytes.Buffer
		b.WriteString(strings.TrimRight(string(content), "\n"))
		b.WriteString("\n\n[" + section + "]\n")
		for _, k := range keys {
			b.WriteString(k + " = " + values[k] + "\n")
		}
		return b.Bytes()
	}

	set := make(map[string]bool, len(values))
	for i := start; i < end; i++ {
		key, ok := lineKey(lines[i])
		if !ok {
			continue
		}
		if v, want := values[key]; want {
			lines[i] = key + " = " + v
			set[key] = true
		}
	}

	var missing []string
	for _, k := range keys {
		if !set[k] {
			missing = append(missing, k+" = "+values[k])
		}
	}
	out := slices.Concat(lines[:start], missing, lines[start:])
	return []byte(strings.Join(out, "\n"))
}

// tableName reports whether line is a table header and returns its name.
func tableName(line string) (string, bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "[") {
		return "", false
	}
	if i := strings.Index(s, "#"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = strings.TrimPrefix(strings.TrimSuffix(s, "]"), "[")
	s = strings.TrimPrefix(strings.TrimSuffix(s, "]"), "[") // array of tables
	return strings.TrimSpace(s), true
}

// lineKey returns the bare key a `key = value` line assigns.
func lineKey(line string) (string, bool) {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "#") {
		return "", false
	}
	key, _, ok := strings.Cut(s, "=")
	if !ok {
		return "", false
	}
	return strings.TrimSpace(key), true
}
