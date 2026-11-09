// Command teeism-identity prints the digest of a pinned enclave identity, or of a set of
// them.
//
// An ISM's trusted state carries this digest, and the module refuses a state whose digest
// does not match the identities it pins. So the digest has to be known before the genesis
// state is built, which is earlier than any transaction exists to ask the chain for it.
//
// One -identity prints that identity's digest. Several print the digest of the set, which
// is what an ISM pinning all of them carries (types.IdentitySetDigest).
package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
)

type identityJSON struct {
	MrTd        string `json:"mr_td"`
	Rtmr0       string `json:"rtmr0"`
	Rtmr1       string `json:"rtmr1"`
	Rtmr2       string `json:"rtmr2"`
	OsImageHash string `json:"os_image_hash"`
	ComposeHash string `json:"compose_hash"`
	KeyProvider string `json:"key_provider"`
}

// paths collects every -identity flag.
type paths []string

func (p *paths) String() string     { return strings.Join(*p, ",") }
func (p *paths) Set(v string) error { *p = append(*p, v); return nil }

func main() {
	var files paths
	flag.Var(&files, "identity", "identity JSON as written by `tee-hyperlane identity --json`, or - for stdin; repeat for a set (up to 8)")
	flag.Parse()
	if len(files) == 0 {
		files = paths{"-"}
	}

	if err := run(files); err != nil {
		fmt.Fprintf(os.Stderr, "teeism-identity: %v\n", err)
		os.Exit(1)
	}
}

func run(files []string) error {
	ids := make([]*types.EnclaveIdentity, len(files))
	for i, path := range files {
		if path == "-" && len(files) > 1 {
			return fmt.Errorf("stdin (-) only works for a single identity")
		}
		id, err := read(path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		ids[i] = id
	}
	digest, err := types.IdentitySetDigest(ids)
	if err != nil {
		return err
	}
	fmt.Printf("0x%s\n", hex.EncodeToString(digest[:]))
	return nil
}

func read(path string) (*types.EnclaveIdentity, error) {
	var raw []byte
	var err error
	if path == "-" {
		raw, err = os.ReadFile("/dev/stdin")
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}

	var in identityJSON
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("parsing identity: %w", err)
	}

	id := &types.EnclaveIdentity{}
	for _, f := range []struct {
		name string
		src  string
		dst  *[]byte
	}{
		{"mr_td", in.MrTd, &id.MrTd},
		{"rtmr0", in.Rtmr0, &id.Rtmr0},
		{"rtmr1", in.Rtmr1, &id.Rtmr1},
		{"rtmr2", in.Rtmr2, &id.Rtmr2},
		{"os_image_hash", in.OsImageHash, &id.OsImageHash},
		{"compose_hash", in.ComposeHash, &id.ComposeHash},
		{"key_provider", in.KeyProvider, &id.KeyProvider},
	} {
		v, err := hex.DecodeString(strings.TrimPrefix(f.src, "0x"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.name, err)
		}
		*f.dst = v
	}

	if err := id.Validate(); err != nil {
		return nil, err
	}
	return id, nil
}
