// Command manifest builds, signs and verifies the manifest of a fastsync changeset bundle.
//
//	manifest keygen -key bundle.key -pub bundle.pub
//	manifest build  -dir BUNDLE -chain-id ID -last-height H (-app-hash HEX | -expected expected.tsv) [-out manifest.json]
//	manifest sign   -manifest manifest.json -key bundle.key [-out manifest.json.sig]
//	manifest verify -dir BUNDLE -manifest manifest.json -sig manifest.json.sig -pub bundle.pub [-chain-id ID]
//
// verify checks the detached ed25519 signature over the exact manifest bytes first, then requires the
// directory to contain exactly the listed files (no unexpected files, no symlinks or subdirectories)
// with the listed sizes and sha256 sums. Exit status 0 = bundle verified; anything else = do not use it.
package main

import (
	"crypto/ed25519"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/manifest"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = cmdKeygen(os.Args[2:])
	case "build":
		err = cmdBuild(os.Args[2:])
	case "sign":
		err = cmdSign(os.Args[2:])
	case "verify":
		err = cmdVerify(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "manifest:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: manifest keygen|build|sign|verify [flags]  (-h for flags)")
	os.Exit(2)
}

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func defaultWorkers() int { return min(4, runtime.NumCPU()) }

func cmdKeygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	key := fs.String("key", "bundle.key", "private key output (PEM PKCS#8, mode 0600; never published)")
	pub := fs.String("pub", "bundle.pub", "public key output (PEM PKIX; publish out of band)")
	_ = fs.Parse(args)
	if err := GenerateKey(*key, *pub); err != nil {
		return err
	}
	fmt.Printf("wrote %s and %s\n", *key, *pub)
	return nil
}

func cmdBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	dir := fs.String("dir", "", "bundle directory (cs-<height>[-<seq>].bin[.zst] files)")
	chainID := fs.String("chain-id", "", "chain id")
	last := fs.Int64("last-height", 0, "last height recorded in the bundle")
	appHash := fs.String("app-hash", "", "hex app hash after -last-height (AppHash of header last-height+1)")
	expected := fs.String("expected", "", "take the app hash from this expected-hash TSV (fshdr verify output) instead of -app-hash")
	ranges := fs.String("ranges", "", "TSV name<TAB>first<TAB>last with each file's real heights (needed when files overlap)")
	out := fs.String("out", "manifest.json", "manifest output path (outside the bundle directory, or add it to -ignore)")
	workers := fs.Int("j", defaultWorkers(), "files hashed in parallel")
	var extra, ignore listFlag
	fs.Var(&extra, "extra", "extra file in the bundle to include without a height range (repeatable)")
	fs.Var(&ignore, "ignore", "directory entry to skip (repeatable)")
	_ = fs.Parse(args)
	if *dir == "" {
		return fmt.Errorf("-dir required")
	}
	if (*appHash == "") == (*expected == "") {
		return fmt.Errorf("give exactly one of -app-hash and -expected")
	}
	if *expected != "" {
		h, err := LookupExpected(*expected, *last)
		if err != nil {
			return err
		}
		*appHash = h
	}
	o := manifest.BuildOptions{
		ChainID: *chainID, LastHeight: *last, LastAppHash: *appHash, Extra: extra,
		Ignore: append(ignore, filepath.Base(*out), filepath.Base(*out)+".sig"), Workers: *workers, Now: time.Now(),
	}
	if *ranges != "" {
		r, err := ReadRanges(*ranges)
		if err != nil {
			return err
		}
		o.Ranges = r
	}
	m, err := manifest.Build(*dir, o)
	if err != nil {
		return err
	}
	b, err := m.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s: chain %s heights %d..%d, %d files, %d bytes, last app hash %s\n",
		*out, m.ChainID, m.FirstHeight, m.LastHeight, len(m.Files), m.TotalBytes, m.LastAppHash)
	return nil
}

func cmdSign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	mf := fs.String("manifest", "manifest.json", "manifest to sign")
	key := fs.String("key", "", "ed25519 private key (PEM)")
	out := fs.String("out", "", "signature output (default <manifest>.sig)")
	_ = fs.Parse(args)
	if *out == "" {
		*out = *mf + ".sig"
	}
	priv, err := LoadPrivateKey(*key)
	if err != nil {
		return err
	}
	b, err := manifest.ReadLimited(*mf, manifest.MaxManifestBytes)
	if err != nil {
		return err
	}
	if _, err := manifest.Parse(b); err != nil {
		return fmt.Errorf("refusing to sign: %w", err)
	}
	sig := ed25519.Sign(priv, b)
	if err := os.WriteFile(*out, EncodeSignature(sig), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", *out)
	return nil
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	dir := fs.String("dir", "", "bundle directory")
	mf := fs.String("manifest", "manifest.json", "manifest")
	sigPath := fs.String("sig", "", "detached signature (default <manifest>.sig)")
	pubPath := fs.String("pub", "", "trusted ed25519 public key (PEM), obtained out of band")
	chainID := fs.String("chain-id", "", "require this chain id")
	allowExtra := fs.Bool("allow-extra", false, "tolerate files that are not in the manifest")
	workers := fs.Int("j", defaultWorkers(), "files hashed in parallel")
	envOut := fs.Bool("print-env", false, "on success print KEY=VALUE lines (CHAIN_ID, FIRST_HEIGHT, LAST_HEIGHT, LAST_APP_HASH) for scripts")
	var ignore listFlag
	fs.Var(&ignore, "ignore", "directory entry to skip (repeatable)")
	_ = fs.Parse(args)
	if *dir == "" || *pubPath == "" {
		return fmt.Errorf("-dir and -pub required")
	}
	if *sigPath == "" {
		*sigPath = *mf + ".sig"
	}
	pub, err := LoadPublicKey(*pubPath)
	if err != nil {
		return err
	}
	mb, err := manifest.ReadLimited(*mf, manifest.MaxManifestBytes)
	if err != nil {
		return err
	}
	sb, err := manifest.ReadLimited(*sigPath, 4096)
	if err != nil {
		return err
	}
	sig, err := DecodeSignature(sb)
	if err != nil {
		return err
	}
	ignore = append(ignore, filepath.Base(*mf), filepath.Base(*sigPath))
	t0 := time.Now()
	m, err := manifest.Verify(*dir, mb, sig, pub, manifest.VerifyOptions{ChainID: *chainID, Ignore: ignore, AllowExtra: *allowExtra, Workers: *workers})
	if err != nil {
		return fmt.Errorf("BUNDLE NOT VERIFIED: %w", err)
	}
	if *envOut {
		fmt.Printf("CHAIN_ID=%s\nFIRST_HEIGHT=%d\nLAST_HEIGHT=%d\nLAST_APP_HASH=%s\n", m.ChainID, m.FirstHeight, m.LastHeight, m.LastAppHash)
		return nil
	}
	fmt.Printf("OK: %d files, %d bytes verified in %s; chain %s heights %d..%d, app hash after %d = %s\n",
		len(m.Files), m.TotalBytes, time.Since(t0).Round(time.Second), m.ChainID, m.FirstHeight, m.LastHeight, m.LastHeight, m.LastAppHash)
	return nil
}
