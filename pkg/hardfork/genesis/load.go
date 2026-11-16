package genesis

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
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

// LoadExportFile hashes the export file at path and checks the hash before it parses the file.
func LoadExportFile(path string, modules []string, opts Options) (*Export, error) {
	want, err := parseSHA256(opts.ExportSHA256)
	if err != nil {
		return nil, err
	}
	got, err := hashFile(path)
	if err != nil {
		return nil, loadError("", err)
	}
	if err := checkSHA256(want, got); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, loadError("", err)
	}
	defer f.Close()
	// LoadExport hashes again, so a file changed since the first pass is refused.
	return LoadExport(f, modules, opts)
}

// LoadExport streams an export file and checks it against the sorted modules and opts.
// The hash is checked after parsing; LoadExportFile checks it first.
func LoadExport(r io.Reader, modules []string, opts Options) (*Export, error) {
	want, err := parseSHA256(opts.ExportSHA256)
	if err != nil {
		return nil, err
	}

	src := &errReader{r: r}
	hash := sha256.New()
	dec := json.NewDecoder(io.TeeReader(src, hash))
	var moduleErr error
	g, consensus, err := decodeGenesis(dec, modules, &moduleErr)
	if err != nil {
		return nil, err
	}
	// Reading to EOF through the tee puts the whole file into the hash.
	if _, err := dec.Token(); err == nil {
		return nil, &CheckError{Step: stepLoad, Want: "end of file", Got: "trailing data"}
	} else if err != io.EOF {
		return nil, loadError("", err)
	}
	if src.err != nil {
		return nil, loadError("", src.err)
	}

	exp := &Export{Genesis: *g}
	hash.Sum(exp.SHA256[:0])
	if err := checkSHA256(want, exp.SHA256); err != nil {
		return nil, err
	}
	if consensus != nil {
		if exp.Consensus, err = decodeConsensus(consensus); err != nil {
			return nil, err
		}
	}
	// The app version comes first: a wrong one explains a different module set.
	if err := checkVersion(exp.Consensus); err != nil {
		return nil, err
	}
	if moduleErr != nil {
		return nil, moduleErr
	}
	if err := checkInput(&exp.Genesis, opts.ExportHeight); err != nil {
		return nil, err
	}
	return exp, nil
}

func parseSHA256(s string) ([]byte, error) {
	want, err := hex.DecodeString(s)
	if err != nil || len(want) != sha256.Size {
		return nil, &CheckError{Step: stepLoad, Field: "export-sha256", Want: "64 hex characters", Got: s}
	}
	return want, nil
}

func checkSHA256(want []byte, got [sha256.Size]byte) error {
	if [sha256.Size]byte(want) != got {
		return &CheckError{Step: stepLoad, Field: "export-sha256", Want: hex.EncodeToString(want), Got: hex.EncodeToString(got[:])}
	}
	return nil
}

// hashFile returns the SHA-256 of the file at path, reading it in constant memory.
func hashFile(path string) (sum [sha256.Size]byte, err error) {
	f, err := os.Open(path)
	if err != nil {
		return sum, err
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	h.Sum(sum[:0])
	return sum, errors.Join(err, f.Close())
}

// decodeGenesis reads the outer object and returns consensus undecoded; unknown keys are rejected.
func decodeGenesis(dec *json.Decoder, modules []string, moduleErr *error) (*Genesis, json.RawMessage, error) {
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
			g.Modules, err = decodeAppState(dec, modules, moduleErr)
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

// decodeAppState reads app_state one module at a time; keys must be strictly ascending.
// An unknown or missing module is set in moduleErr instead, so the app version can be checked first.
func decodeAppState(dec *json.Decoder, modules []string, moduleErr *error) ([]Module, error) {
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
		if _, ok := slices.BinarySearch(modules, name); !ok && *moduleErr == nil {
			*moduleErr = &CheckError{Step: stepLoad, Module: name, Field: "app_state", Want: "a known module", Got: "unknown module"}
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
	if *moduleErr != nil {
		return mods, nil
	}
	// mods is a strictly ascending subset of modules, so the first mismatch is missing.
	for i, name := range modules {
		if i >= len(mods) || mods[i].Name != name {
			*moduleErr = &CheckError{Step: stepLoad, Module: name, Field: "app_state", Want: "present", Got: "missing"}
			break
		}
	}
	return mods, nil
}

// decodeConsensus decodes consensus and rejects repeated keys and keys that the decoder drops.
func decodeConsensus(raw json.RawMessage) (*genutiltypes.ConsensusGenesis, error) {
	var c *genutiltypes.ConsensusGenesis
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, loadError("consensus", err)
	}
	if c == nil {
		// checkVersion reports a null consensus as missing.
		return nil, nil
	}
	// The re-encoding has every key the decoder kept; omitempty drops these two when empty.
	known := map[string]bool{`consensus."validators"`: true, `consensus."params"`: true}
	enc, err := json.Marshal(c)
	if err == nil {
		err = walkKeys(json.NewDecoder(bytes.NewReader(enc)), "consensus", func(p string) error {
			known[p] = true
			return nil
		})
	}
	if err != nil {
		return nil, loadError("consensus", err)
	}
	// Checking each key before descending keeps the walk as shallow as the known keys.
	err = walkKeys(json.NewDecoder(bytes.NewReader(raw)), "consensus", func(p string) error {
		if !known[p] {
			return &CheckError{Step: stepLoad, Field: p, Want: "a known field", Got: "unknown field"}
		}
		return nil
	})
	if err != nil {
		return nil, loadError("consensus", err)
	}
	return c, nil
}

// walkKeys calls fn with the path of each object key in a JSON value, before its value; a repeated key fails.
func walkKeys(dec *json.Decoder, path string, fn func(path string) error) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch tok {
	case json.Delim('{'):
		seen := make(map[string]bool)
		for dec.More() {
			key, err := keyToken(dec)
			if err != nil {
				return err
			}
			// Quoting keeps a key with dots or brackets from matching another path.
			p := path + "." + strconv.Quote(key)
			if seen[key] {
				return &CheckError{Step: stepLoad, Field: p, Want: "one occurrence", Got: "repeated key"}
			}
			seen[key] = true
			if err := fn(p); err != nil {
				return err
			}
			if err := walkKeys(dec, p, fn); err != nil {
				return err
			}
		}
	case json.Delim('['):
		for dec.More() {
			if err := walkKeys(dec, path+"[]", fn); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	_, err = dec.Token()
	return err
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

func loadError(field string, err error) *CheckError {
	if ce, ok := errors.AsType[*CheckError](err); ok {
		return ce
	}
	return &CheckError{Step: stepLoad, Field: field, Got: err.Error()}
}

// checkVersion requires consensus params of this app version.
func checkVersion(c *genutiltypes.ConsensusGenesis) error {
	if c == nil {
		return &CheckError{Step: stepInput, Field: "consensus", Want: "present", Got: "missing"}
	}
	if c.Params == nil {
		return &CheckError{Step: stepInput, Field: "consensus.params", Want: "present", Got: "missing"}
	}
	if v := c.Params.Version.App; v != appconsts.Version {
		got := fmt.Sprint(v)
		if v != 0 {
			got = fmt.Sprintf("%d (use the v%d binary)", v, v)
		}
		return &CheckError{Step: stepInput, Field: "consensus.params.version.app", Want: fmt.Sprint(appconsts.Version), Got: got}
	}
	return nil
}

// checkInput checks the exported outer fields before anything fills defaults; checkVersion runs first.
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
	params := g.Consensus.Params
	if err := params.ValidateBasic(); err != nil {
		return &CheckError{Step: stepInput, Field: "consensus.params", Want: "valid consensus params", Got: err.Error()}
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
