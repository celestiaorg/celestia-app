package maps

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SetField modifies a JSON-encoded byte slice by updating or inserting a value at the given dot-delimited path.
// Returns the updated JSON-encoded byte slice or an error if unmarshalling, marshaling, or setting the field fails.
func SetField(bz []byte, path string, value interface{}) ([]byte, error) {
	var doc map[string]interface{}
	if err := json.Unmarshal(bz, &doc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal genesis: %w", err)
	}

	if err := setOrDeleteNestedField(doc, path, value); err != nil {
		return nil, err
	}

	return json.MarshalIndent(doc, "", "  ")
}

// RemoveField removes a JSON field identified by the path from the provided byte slice and returns the updated JSON or an error.
func RemoveField(bz []byte, path string) ([]byte, error) {
	return SetField(bz, path, nil)
}

// setOrDeleteNestedField modifies a nested field in a map based on a dot-delimited path or deletes it if value is nil.
// Returns an error if the path is invalid or intermediate nodes are not maps.
func setOrDeleteNestedField(doc map[string]interface{}, path string, value interface{}) error {
	keys := strings.Split(path, ".")

	current := doc
	for i, key := range keys {
		// if it's the last key, set the value
		if i == len(keys)-1 {
			if value == nil {
				delete(current, key)
				return nil
			}
			current[key] = value
			return nil
		}

		next, ok := current[key].(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid path: %s is not a map", strings.Join(keys[:i+1], "."))
		}
		current = next
	}
	return nil
}
