// Command fshdr produces trusted app hashes for changeset replay without
// trusting whoever serves the headers.
//
//	fshdr verify  -chain-id ID -from A -to B -out expected.tsv (-rpc URLS | -blockstore DIR)
//	              (-trust-height T -trust-hash HEX [-trust-period 168h] [-anchor-height C] | -anchor-height C -anchor-hash HEX)
//	fshdr forward -chain-id ID -trusted-height T -trusted-hash HEX -to B -out expected.tsv (-rpc URLS | -data DIR)
//	fshdr headers -blockstore DIR [-backend B] -from A -to B
//
// verify walks down from one trusted header:
//
//  1. Anchor. A light client started from the trust root (the same
//     weak-subjectivity input as state sync; primary = first RPC, witnesses =
//     the rest) verifies header B+1, or header C with -anchor-height. With
//     -anchor-hash the operator supplies the hash of header C out of band
//     instead, and no light client or trusting period is involved.
//  2. Hash chain. Headers from the anchor down to A+1 come from an untrusted
//     source (RPC /blockchain, or a block store). Each header's recomputed hash
//     must equal LastBlockID.Hash of the header above it.
//
// forward walks up from one trusted header T to B+1 and checks every height
// as a full node would: hash link, validator-set transition, canonical commit
// binding and +2/3 of the validator set's power signing each commit. It needs
// commits and validator sets, but no light client or trusting period, so it
// can verify from an old checkpoint up to the tip.
//
// Both write one row per height h, from header h+1: h, AppHash, header hash,
// LastResultsHash, ValidatorsHash, NextValidatorsHash, app version. Column 2
// is the app hash of the state after h; column 3 is the hash fsboot needs.
//
// headers prints the same rows from a block store without any verification,
// for comparing a node's own block store with a verified expected.tsv.
package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cometbft/cometbft/light"
	"github.com/cometbft/cometbft/types"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/expected"
	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/hdrchain"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "verify":
		err = cmdVerify(os.Args[2:], os.Stdout)
	case "forward":
		err = cmdForward(os.Args[2:], os.Stdout)
	case "headers":
		err = cmdHeaders(os.Args[2:], os.Stdout)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fshdr: FAILED:", err)
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: fshdr verify|forward|headers [flags]  (-h for flags)")
	os.Exit(1)
}

func logf(format string, a ...any) { fmt.Fprintf(os.Stderr, "fshdr: "+format+"\n", a...) }

func decodeHash(name, s string) ([]byte, error) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		return nil, fmt.Errorf("%s: want 32 bytes of hex", name)
	}
	return b, nil
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

type verifyFlags struct {
	chainID, rpcs, trustHash, anchorHash, bsDir, backend, out string
	trustHeight, anchorHeight, from, to, window               int64
	trustPeriod                                               time.Duration
	parallel                                                  int
}

func parseVerify(args []string) (verifyFlags, error) {
	var f verifyFlags
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.StringVar(&f.chainID, "chain-id", "", "chain id")
	fs.StringVar(&f.rpcs, "rpc", "", "comma-separated RPC servers (first = light client primary, rest = witnesses; header fetches use all)")
	fs.Int64Var(&f.trustHeight, "trust-height", 0, "light client trust height")
	fs.StringVar(&f.trustHash, "trust-hash", "", "hash of the header at -trust-height (hex)")
	fs.DurationVar(&f.trustPeriod, "trust-period", 168*time.Hour, "light client trusting period")
	fs.Int64Var(&f.from, "from", 1, "first height whose post-state app hash is needed")
	fs.Int64Var(&f.to, "to", 0, "last height whose post-state app hash is needed (header to+1 must exist)")
	fs.Int64Var(&f.anchorHeight, "anchor-height", 0, "anchor at this header instead of to+1 (must be > to)")
	fs.StringVar(&f.anchorHash, "anchor-hash", "", "with -anchor-height: operator-supplied hash of that header; no light client")
	fs.StringVar(&f.bsDir, "blockstore", "", "read headers from the block store in this directory instead of RPC")
	fs.StringVar(&f.backend, "backend", "goleveldb", "block store backend")
	fs.StringVar(&f.out, "out", "", "output file (default stdout)")
	fs.IntVar(&f.parallel, "parallel", 8, "concurrent RPC calls")
	fs.Int64Var(&f.window, "window", 50000, "headers verified per window (memory bound)")
	if err := fs.Parse(args); err != nil {
		return f, err
	}
	if f.chainID == "" || f.from < 1 || f.to < f.from {
		return f, errors.New("need -chain-id and 1 <= -from <= -to")
	}
	if f.anchorHeight == 0 {
		f.anchorHeight = f.to + 1
	}
	if f.anchorHeight <= f.to {
		return f, fmt.Errorf("-anchor-height %d must be > -to %d", f.anchorHeight, f.to)
	}
	if f.anchorHash != "" {
		if f.rpcs == "" && f.bsDir == "" {
			return f, errors.New("-anchor-hash needs a header source (-rpc or -blockstore)")
		}
	} else if f.rpcs == "" || f.trustHeight <= 0 || f.trustHash == "" {
		return f, errors.New("need -rpc, -trust-height and -trust-hash (or -anchor-height with -anchor-hash)")
	}
	return f, nil
}

func cmdVerify(args []string, stdout io.Writer) error {
	f, err := parseVerify(args)
	if err != nil {
		return err
	}
	servers := splitList(f.rpcs)
	ctx := context.Background()
	t0 := time.Now()

	// 1. anchor
	var anchor []byte
	if f.anchorHash != "" {
		if anchor, err = decodeHash("-anchor-hash", f.anchorHash); err != nil {
			return err
		}
		logf("anchor header %d hash %X supplied by the operator (no light client)", f.anchorHeight, anchor)
	} else {
		th, err := decodeHash("-trust-hash", f.trustHash)
		if err != nil {
			return err
		}
		if len(servers) == 1 {
			logf("WARNING: one RPC server only; it is also its own light-client witness")
		}
		trust := light.TrustOptions{Period: f.trustPeriod, Height: f.trustHeight, Hash: th}
		if anchor, err = hdrchain.LightClientAnchor(ctx, f.chainID, trust, servers, f.anchorHeight); err != nil {
			return err
		}
		logf("anchor header %d hash %X verified by the light client from trust root %d", f.anchorHeight, anchor, f.trustHeight)
	}

	// 2. untrusted header source
	var src hdrchain.RangeSource
	if f.bsDir != "" {
		st, err := hdrchain.OpenStores(f.bsDir, f.backend, false)
		if err != nil {
			return err
		}
		defer st.Close()
		src = st
		logf("header source: block store %s (untrusted, hash-chain checked)", f.bsDir)
	} else {
		src = hdrchain.Parallel(hdrchain.NewHTTPSource(servers...), f.parallel, hdrchain.MaxBlockchainMetas)
		logf("header source: RPC %v (untrusted, hash-chain checked)", servers)
	}

	// 3. verify, writing each window to its own part file (windows arrive top down)
	w, err := newPartWriter(f.out)
	if err != nil {
		return err
	}
	defer w.cleanup()
	p := expected.Params{ChainID: f.chainID, AnchorHeight: f.anchorHeight, AnchorHash: anchor, From: f.from, To: f.to, Window: f.window}
	err = expected.Walk(ctx, p, src, func(rows []expected.Row) error {
		if len(rows) > 0 {
			logf("verified heights %d..%d", rows[0].Height, rows[len(rows)-1].Height)
		}
		return w.part(rows)
	})
	if err != nil {
		return err
	}
	header := expected.Header("verify", f.chainID, fmt.Sprintf("anchor=%d:%X", f.anchorHeight, anchor))
	if err := w.finish(header, stdout); err != nil {
		return err
	}
	logf("OK: %d trusted app hashes (heights %d..%d) in %s", f.to-f.from+1, f.from, f.to, time.Since(t0).Round(time.Millisecond))
	return nil
}

func cmdForward(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("forward", flag.ContinueOnError)
	chainID := fs.String("chain-id", "", "chain id")
	rpcs := fs.String("rpc", "", "comma-separated RPC servers (untrusted; /commit and /validators)")
	data := fs.String("data", "", "read headers, commits and validator sets from this stopped node's data dir instead of RPC")
	backend := fs.String("backend", "goleveldb", "-data backend")
	trustedHeight := fs.Int64("trusted-height", 0, "height T of the trusted header (rows start at T)")
	trustedHash := fs.String("trusted-hash", "", "out-of-band hash of header T (hex)")
	to := fs.Int64("to", 0, "last row (header to+1 is verified)")
	allSigs := fs.Bool("all-signatures", false, "check every signature of each commit, not only +2/3")
	out := fs.String("out", "", "output file (default stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *chainID == "" || *trustedHeight < 1 || *to < *trustedHeight || (*rpcs == "") == (*data == "") {
		return errors.New("need -chain-id, 1 <= -trusted-height <= -to and exactly one of -rpc/-data")
	}
	th, err := decodeHash("-trusted-hash", *trustedHash)
	if err != nil {
		return err
	}
	var src hdrchain.Source
	if *data != "" {
		st, err := hdrchain.OpenStores(*data, *backend, true)
		if err != nil {
			return err
		}
		defer st.Close()
		src = st
	} else {
		src = hdrchain.NewHTTPSource(splitList(*rpcs)...)
	}
	ctx := context.Background()
	t0 := time.Now()
	sh, err := src.SignedHeader(ctx, *trustedHeight)
	if err != nil {
		return err
	}
	vals, err := src.Validators(ctx, *trustedHeight)
	if err != nil {
		return err
	}
	v, err := hdrchain.NewTrustedVerifier(*chainID, sh, th, vals)
	if err != nil {
		return err
	}
	v.AllSignatures = *allSigs
	v.OnAppVersion = func(h int64, from, to uint64) { logf("height %d: app version %d -> %d", h, from, to) }

	// rows arrive in ascending order: stream them to the output
	o, err := newOutput(*out, stdout)
	if err != nil {
		return err
	}
	defer o.abort()
	fmt.Fprintln(o.w, expected.Header("forward", *chainID, fmt.Sprintf("trusted=%d:%X", *trustedHeight, th)))
	st, err := hdrchain.RunForward(ctx, src, v, *trustedHeight+1, *to+1, func(sh *types.SignedHeader) error {
		if sh.Height%50000 == 0 {
			logf("verified height %d", sh.Height)
		}
		_, err := fmt.Fprintln(o.w, expected.FromHeader(sh.Header).String())
		return err
	})
	if err != nil {
		return err
	}
	if err := o.commit(); err != nil {
		return err
	}
	logf("OK: %d headers verified (%d validator sets fetched), rows %d..%d in %s",
		st.Headers, st.ValidatorSets, *trustedHeight, *to, time.Since(t0).Round(time.Millisecond))
	return nil
}

// output writes to a temporary file renamed into place by commit (or to
// stdout when no file is given), so a failed run leaves no output file.
type output struct {
	w    *bufio.Writer
	path string
	f    *os.File
}

func newOutput(path string, stdout io.Writer) (*output, error) {
	if path == "" {
		return &output{w: bufio.NewWriter(stdout)}, nil
	}
	f, err := os.Create(path + ".tmp")
	if err != nil {
		return nil, err
	}
	return &output{w: bufio.NewWriter(f), path: path, f: f}, nil
}

func (o *output) commit() error {
	if err := o.w.Flush(); err != nil || o.f == nil {
		return err
	}
	if err := errors.Join(o.f.Sync(), o.f.Close()); err != nil {
		return err
	}
	o.f = nil
	return os.Rename(o.path+".tmp", o.path)
}

func (o *output) abort() {
	if o.f != nil {
		_ = o.f.Close()
		_ = os.Remove(o.path + ".tmp")
	}
}

// partWriter keeps each window's rows in a part file and joins them, last
// part first, so windows verified top down come out in ascending order.
type partWriter struct {
	out, dir string
	parts    []string
}

func newPartWriter(out string) (*partWriter, error) {
	base := os.TempDir()
	if out != "" {
		base = filepath.Dir(out)
	}
	dir, err := os.MkdirTemp(base, "fshdr-parts-")
	if err != nil {
		return nil, err
	}
	return &partWriter{out: out, dir: dir}, nil
}

func (w *partWriter) part(rows []expected.Row) error {
	name := filepath.Join(w.dir, fmt.Sprintf("%08d", len(w.parts)))
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(f)
	for _, r := range rows {
		fmt.Fprintln(bw, r.String())
	}
	if err := errors.Join(bw.Flush(), f.Close()); err != nil {
		return err
	}
	w.parts = append(w.parts, name)
	return nil
}

func (w *partWriter) finish(header string, stdout io.Writer) error {
	o, err := newOutput(w.out, stdout)
	if err != nil {
		return err
	}
	defer o.abort()
	fmt.Fprintln(o.w, header)
	for i := len(w.parts) - 1; i >= 0; i-- {
		f, err := os.Open(w.parts[i])
		if err != nil {
			return err
		}
		_, err = io.Copy(o.w, f)
		f.Close()
		if err != nil {
			return err
		}
	}
	return o.commit()
}

func (w *partWriter) cleanup() { _ = os.RemoveAll(w.dir) }

func cmdHeaders(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("headers", flag.ContinueOnError)
	dir := fs.String("blockstore", "", "directory containing blockstore.db")
	backend := fs.String("backend", "goleveldb", "block store backend")
	from := fs.Int64("from", 0, "first row (default: block store base)")
	to := fs.Int64("to", 0, "last row (default: block store height - 1)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return errors.New("-blockstore is required")
	}
	st, err := hdrchain.OpenStores(*dir, *backend, false)
	if err != nil {
		return err
	}
	defer st.Close()
	base, height := st.Blocks.Base(), st.Blocks.Height()
	if *from == 0 {
		*from = base
	}
	if *to == 0 {
		*to = height - 1
	}
	if *from < 1 || *to < *from {
		return fmt.Errorf("empty range %d..%d (block store %d..%d)", *from, *to, base, height)
	}
	hs, err := st.Headers(context.Background(), *from+1, *to+1)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(stdout)
	fmt.Fprintf(bw, "# fshdr headers (UNVERIFIED) block store base=%d height=%d\n", base, height)
	for _, h := range hs {
		fmt.Fprintln(bw, expected.FromHeader(h).String())
	}
	return bw.Flush()
}
