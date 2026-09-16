// Command teeism-identity prints the digest of a pinned enclave identity.
//
// An ISM's trusted state carries this digest, and the module refuses a state whose digest
// does not match the identity it pins. So the digest has to be known before the genesis
// state is built, which is earlier than any transaction exists to ask the chain for it.
package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/celestiaorg/celestia-app/v10/x/teeism/types"
)

type identityJSON struct {
	MrTd        string `json:"mr_td"`
	OsImageHash string `json:"os_image_hash"`
	ComposeHash string `json:"compose_hash"`
	MrKms       string `json:"mr_kms"`
	KeyProvider string `json:"key_provider"`
}

func main() {
	path := flag.String("identity", "-", "identity JSON as written by `circuit-tool identity --json`, or - for stdin")
	flag.Parse()

	if err := run(*path); err != nil {
		fmt.Fprintf(os.Stderr, "teeism-identity: %v\n", err)
		os.Exit(1)
	}
}

func run(path string) error {
	var raw []byte
	var err error
	if path == "-" {
		raw, err = os.ReadFile("/dev/stdin")
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return err
	}

	var in identityJSON
	if err := json.Unmarshal(raw, &in); err != nil {
		return fmt.Errorf("parsing identity: %w", err)
	}

	id := &types.EnclaveIdentity{}
	for _, f := range []struct {
		name string
		src  string
		dst  *[]byte
	}{
		{"mr_td", in.MrTd, &id.MrTd},
		{"os_image_hash", in.OsImageHash, &id.OsImageHash},
		{"compose_hash", in.ComposeHash, &id.ComposeHash},
		{"mr_kms", in.MrKms, &id.MrKms},
		{"key_provider", in.KeyProvider, &id.KeyProvider},
	} {
		v, err := hex.DecodeString(strings.TrimPrefix(f.src, "0x"))
		if err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
		*f.dst = v
	}

	if err := id.Validate(); err != nil {
		return err
	}

	digest := id.Digest()
	fmt.Printf("0x%s\n", hex.EncodeToString(digest[:]))
	return nil
}
