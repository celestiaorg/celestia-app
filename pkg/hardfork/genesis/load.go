package genesis

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
)

const (
	stepLoad  = "load"
	stepInput = "input"
	// maxDepth is the whole-document nesting limit of encoding/json.
	maxDepth = 10000
)

// errMaxDepth has the text that encoding/json v2 uses for the same limit.
var errMaxDepth = errors.New("exceeded max depth")

// errReader records the first read error other than io.EOF.
type errReader struct {
	r   io.Reader
	err error
}

func (e *errReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	// Only the exact io.EOF sentinel is a clean end.
	if err != nil && err != io.EOF && e.err == nil {
		e.err = err
	}
	return n, err
}

// LoadExport streams an export file and checks it against the sorted modules and opts.
func LoadExport(r io.Reader, modules []string, opts Options) (*Export, error) {
	want, err := hex.DecodeString(opts.ExportSHA256)
	if err != nil || len(want) != sha256.Size {
		return nil, &CheckError{Step: stepLoad, Field: "export-sha256", Want: "64 hex characters", Got: opts.ExportSHA256}
	}

	src := &errReader{r: r}
	hash := sha256.New()
	tee := io.TeeReader(src, hash)
	dec := json.NewDecoder(tee)
	g, consensus, err := decodeGenesis(dec, modules)
	if err != nil {
		return nil, err
	}
	// Reading the rest through the tee also puts it into the hash.
	if err := checkEnd(io.MultiReader(dec.Buffered(), tee)); err != nil {
		return nil, err
	}
	if src.err != nil {
		return nil, loadError("", src.err)
	}

	exp := &Export{Genesis: *g}
	hash.Sum(exp.SHA256[:0])
	if !bytes.Equal(exp.SHA256[:], want) {
		return nil, &CheckError{Step: stepLoad, Field: "export-sha256", Want: hex.EncodeToString(want), Got: hex.EncodeToString(exp.SHA256[:])}
	}
	if consensus != nil {
		if err := json.Unmarshal(consensus, &exp.Consensus); err != nil {
			return nil, loadError("consensus", err)
		}
	}
	if err := checkInput(&exp.Genesis, opts.ExportHeight); err != nil {
		return nil, err
	}
	return exp, nil
}

// decodeGenesis reads the outer object and returns consensus undecoded; unknown keys are rejected.
func decodeGenesis(dec *json.Decoder, modules []string) (*Genesis, json.RawMessage, error) {
	if err := expectDelim(dec, '{'); err != nil {
		return nil, nil, loadError("", err)
	}
	g := &Genesis{}
	var consensus json.RawMessage
	seen := make(map[string]bool)
	for dec.More() {
		key, err := keyToken(dec)
		if err != nil {
			return nil, nil, loadError("", err)
		}
		if seen[key] {
			return nil, nil, &CheckError{Step: stepLoad, Field: key, Want: "one occurrence", Got: "repeated key"}
		}
		seen[key] = true

		switch key {
		case "app_name":
			err = dec.Decode(&g.AppName)
		case "app_version":
			err = dec.Decode(&g.AppVersion)
		case "genesis_time":
			err = dec.Decode(&g.GenesisTime)
		case "chain_id":
			err = dec.Decode(&g.ChainID)
		case "initial_height":
			err = dec.Decode(&g.InitialHeight)
		case "app_hash":
			err = dec.Decode(&g.AppHash)
		case "app_state":
			g.Modules, err = decodeAppState(dec, modules)
		case "consensus":
			if err = dec.Decode(&consensus); err == nil && depth(consensus)+1 > maxDepth {
				err = errMaxDepth
			}
		default:
			return nil, nil, &CheckError{Step: stepLoad, Field: key, Want: "an AppGenesis key", Got: "unknown key"}
		}
		if err != nil {
			return nil, nil, loadError(key, err)
		}
	}
	if err := expectDelim(dec, '}'); err != nil {
		return nil, nil, loadError("", err)
	}
	if !seen["app_state"] {
		return nil, nil, &CheckError{Step: stepLoad, Field: "app_state", Want: "present", Got: "missing"}
	}
	return g, consensus, nil
}

// decodeAppState reads app_state one module at a time; keys must be strictly ascending and in modules.
func decodeAppState(dec *json.Decoder, modules []string) ([]Module, error) {
	if err := expectDelim(dec, '{'); err != nil {
		return nil, err
	}
	var mods []Module
	for dec.More() {
		name, err := keyToken(dec)
		if err != nil {
			return nil, err
		}
		if n := len(mods); n > 0 && name <= mods[n-1].Name {
			return nil, &CheckError{Step: stepLoad, Module: name, Field: "app_state", Want: "a key after " + mods[n-1].Name, Got: name}
		}
		if _, ok := slices.BinarySearch(modules, name); !ok {
			return nil, &CheckError{Step: stepLoad, Module: name, Field: "app_state", Want: "a known module", Got: "unknown module"}
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		// The outer object and app_state add two levels.
		if depth(raw)+2 > maxDepth {
			return nil, errMaxDepth
		}
		mods = append(mods, Module{Name: name, Raw: raw})
	}
	if err := expectDelim(dec, '}'); err != nil {
		return nil, err
	}
	// mods is a strictly ascending subset of modules, so the first mismatch is missing.
	for i, name := range modules {
		if i >= len(mods) || mods[i].Name != name {
			return nil, &CheckError{Step: stepLoad, Module: name, Field: "app_state", Want: "present", Got: "missing"}
		}
	}
	return mods, nil
}

// depth returns the deepest array or object nesting in a valid JSON value.
func depth(raw []byte) int {
	cur, deepest := 0, 0
	inString, escaped := false, false
	for _, c := range raw {
		switch {
		case escaped:
			escaped = false
		case inString:
			escaped = c == '\\'
			inString = c != '"'
		case c == '"':
			inString = true
		case c == '[' || c == '{':
			cur++
			deepest = max(deepest, cur)
		case c == ']' || c == '}':
			cur--
		}
	}
	return deepest
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("want %q, got %v", want, tok)
	}
	return nil
}

func keyToken(dec *json.Decoder) (string, error) {
	tok, err := dec.Token()
	if err != nil {
		return "", err
	}
	key, ok := tok.(string)
	if !ok {
		return "", fmt.Errorf("want an object key, got %v", tok)
	}
	return key, nil
}

// checkEnd reads r to EOF and fails on anything but JSON whitespace.
func checkEnd(r io.Reader) error {
	br := bufio.NewReader(r)
	for {
		c, err := br.ReadByte()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return loadError("", err)
		}
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			return &CheckError{Step: stepLoad, Want: "end of file", Got: "trailing data"}
		}
	}
}

func loadError(field string, err error) *CheckError {
	if ce, ok := errors.AsType[*CheckError](err); ok {
		return ce
	}
	return &CheckError{Step: stepLoad, Field: field, Got: err.Error()}
}

// checkInput checks the exported outer fields before anything fills defaults.
func checkInput(g *Genesis, exportHeight int64) error {
	if g.ChainID == "" {
		return &CheckError{Step: stepInput, Field: "chain_id", Want: "non-empty", Got: `""`}
	}
	if g.InitialHeight < 2 {
		return &CheckError{Step: stepInput, Field: "initial_height", Want: ">= 2", Got: fmt.Sprint(g.InitialHeight)}
	}
	if exportHeight != g.InitialHeight-1 {
		return &CheckError{Step: stepInput, Field: "export-height", Want: fmt.Sprint(g.InitialHeight - 1), Got: fmt.Sprint(exportHeight)}
	}
	if g.Consensus == nil {
		return &CheckError{Step: stepInput, Field: "consensus", Want: "present", Got: "missing"}
	}
	params := g.Consensus.Params
	if params == nil {
		return &CheckError{Step: stepInput, Field: "consensus.params", Want: "present", Got: "missing"}
	}
	if v := params.Version.App; v != appconsts.Version {
		got := fmt.Sprint(v)
		if v != 0 {
			got = fmt.Sprintf("%d (use the v%d binary)", v, v)
		}
		return &CheckError{Step: stepInput, Field: "consensus.params.version.app", Want: fmt.Sprint(appconsts.Version), Got: got}
	}
	if len(g.Consensus.Validators) == 0 {
		return &CheckError{Step: stepInput, Field: "consensus.validators", Want: "non-empty", Got: "empty"}
	}
	for i, v := range g.Consensus.Validators {
		if v.Power <= 0 {
			return &CheckError{Step: stepInput, Field: fmt.Sprintf("consensus.validators[%d].power", i), Want: "> 0", Got: fmt.Sprint(v.Power)}
		}
	}
	if err := checkValidators(params, g.Consensus.Validators); err != nil {
		return err
	}
	if len(g.AppHash) != 0 {
		return &CheckError{Step: stepInput, Field: "app_hash", Want: "empty", Got: hex.EncodeToString(g.AppHash)}
	}
	return nil
}
