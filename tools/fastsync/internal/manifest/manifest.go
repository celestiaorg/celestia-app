// Package manifest builds and verifies the signed manifest of a fastsync changeset bundle:
// per-file name, size, sha256 and height range, plus the chain id and the app hash at the last height.
package manifest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FormatVersion is the manifest format version written by build and accepted by verify.
const FormatVersion = 1

// MaxManifestBytes bounds how much of a manifest or signature file is read.
const MaxManifestBytes = 64 << 20

// maxFiles bounds the number of entries in a manifest (the mainnet bundle has ~1.6k files).
const maxFiles = 1 << 20

// Manifest describes a changeset bundle: one directory of cs-<height>[-<seq>].bin[.zst] files
// (plus optional extra files such as an expected-hash TSV) up to LastHeight.
type Manifest struct {
	Version     int    `json:"version"`
	ChainID     string `json:"chain_id"`
	FirstHeight int64  `json:"first_height"`
	LastHeight  int64  `json:"last_height"`
	// LastAppHash is the app hash after executing LastHeight, i.e. the AppHash in header
	// LastHeight+1 (the same convention as the expected-hash TSV written by fshdr).
	LastAppHash string `json:"last_app_hash"`
	CreatedAt   string `json:"created_at,omitempty"`
	TotalBytes  int64  `json:"total_bytes"`
	Files       []File `json:"files"`
}

// File is one bundle file. Height fields are zero for extra (non-changeset) files.
type File struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	FirstHeight int64  `json:"first_height,omitempty"`
	LastHeight  int64  `json:"last_height,omitempty"`
}

var (
	// safeName is the only accepted shape of a file name in a manifest: a plain base name.
	safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,254}$`)
	// changesetName matches the recorder's output names (see store/changeset recorder).
	changesetName = regexp.MustCompile(`^cs-([0-9]{1,19})(?:-([0-9]{1,9}))?\.bin(\.zst)?$`)
)

func checkName(name string) error {
	if !safeName.MatchString(name) || name == "." || name == ".." || strings.Contains(name, "..") {
		return fmt.Errorf("unsafe file name %q", name)
	}
	return nil
}

type csFile struct {
	name     string
	first    int64
	seq      int64
	explicit bool // range from -ranges
	last     int64
}

func parseChangesetName(name string) (first, seq int64, ok bool) {
	m := changesetName.FindStringSubmatch(name)
	if m == nil {
		return 0, 0, false
	}
	first, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || first < 1 {
		return 0, 0, false
	}
	if m[2] != "" {
		if seq, err = strconv.ParseInt(m[2], 10, 64); err != nil {
			return 0, 0, false
		}
	}
	return first, seq, true
}

// BuildOptions configures Build.
type BuildOptions struct {
	ChainID     string
	LastHeight  int64
	LastAppHash string              // hex; 32 bytes
	Extra       []string            // extra (non-changeset) file names to include, e.g. expected.tsv.zst
	Ignore      []string            // names in the directory that are neither listed nor rejected
	Ranges      map[string][2]int64 // optional explicit per-file [first,last] heights
	Workers     int
	Now         time.Time
}

// Build hashes every file of dir and returns its manifest. Any file in dir that is not a
// changeset file, not listed in Extra and not in Ignore is an error (e.g. a leftover
// *.zst.tmp from an interrupted compression), so a published bundle is exactly what was hashed.
func Build(dir string, o BuildOptions) (*Manifest, error) {
	if o.ChainID == "" {
		return nil, errors.New("chain id required")
	}
	if o.LastHeight < 1 {
		return nil, errors.New("last height required")
	}
	if err := checkHash32(o.LastAppHash); err != nil {
		return nil, fmt.Errorf("last app hash: %w", err)
	}
	extra := map[string]bool{}
	for _, n := range o.Extra {
		if err := checkName(n); err != nil {
			return nil, err
		}
		extra[n] = true
	}
	ignore := setOf(o.Ignore)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var cs []csFile
	var extras []string
	byBase := map[string]string{}
	for _, e := range ents {
		name := e.Name()
		if ignore[name] {
			continue
		}
		if !e.Type().IsRegular() {
			return nil, fmt.Errorf("%s: not a regular file (directories and symlinks are not allowed in a bundle)", name)
		}
		if err := checkName(name); err != nil {
			return nil, err
		}
		if extra[name] {
			extras = append(extras, name)
			delete(extra, name)
			continue
		}
		first, seq, ok := parseChangesetName(name)
		if !ok {
			return nil, fmt.Errorf("%s: unexpected file (not cs-<height>[-<seq>].bin[.zst]; pass -extra or -ignore, or remove it)", name)
		}
		base := strings.TrimSuffix(name, ".zst")
		if other, dup := byBase[base]; dup {
			return nil, fmt.Errorf("%s and %s hold the same changeset file: remove one (the applier prefers .zst)", other, name)
		}
		byBase[base] = name
		f := csFile{name: name, first: first, seq: seq}
		if r, ok := o.Ranges[name]; ok {
			if r[0] != first || r[1] < r[0] {
				return nil, fmt.Errorf("%s: range %d..%d does not start at the file's height %d", name, r[0], r[1], first)
			}
			f.explicit, f.last = true, r[1]
		}
		cs = append(cs, f)
	}
	for n := range extra {
		return nil, fmt.Errorf("extra file %s not found in %s", n, dir)
	}
	if len(cs) == 0 {
		return nil, fmt.Errorf("no changeset files in %s", dir)
	}
	if len(cs)+len(extras) > maxFiles {
		return nil, fmt.Errorf("too many files (%d)", len(cs)+len(extras))
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].first != cs[j].first {
			return cs[i].first < cs[j].first
		}
		return cs[i].seq < cs[j].seq
	})
	// Per-file heights: explicit (-ranges) or inferred from the next file's first height. Inference
	// needs strictly increasing first heights (no restart overlap); the last file ends at LastHeight.
	for i := range cs {
		if cs[i].explicit {
			continue
		}
		if i == len(cs)-1 {
			cs[i].last = o.LastHeight
			continue
		}
		if cs[i+1].first <= cs[i].first {
			return nil, fmt.Errorf("%s and %s start at the same height (recorder restart): pass -ranges with each file's real height range", cs[i].name, cs[i+1].name)
		}
		cs[i].last = cs[i+1].first - 1
	}
	covered := cs[0].first - 1
	for _, f := range cs {
		if f.first > covered+1 {
			return nil, fmt.Errorf("gap: heights %d..%d are in no file (next file %s)", covered+1, f.first-1, f.name)
		}
		covered = max(covered, f.last)
	}
	if covered != o.LastHeight {
		return nil, fmt.Errorf("files cover heights %d..%d but last height is %d", cs[0].first, covered, o.LastHeight)
	}

	m := &Manifest{
		Version:     FormatVersion,
		ChainID:     o.ChainID,
		FirstHeight: cs[0].first,
		LastHeight:  o.LastHeight,
		LastAppHash: strings.ToUpper(o.LastAppHash),
	}
	if !o.Now.IsZero() {
		m.CreatedAt = o.Now.UTC().Format(time.RFC3339)
	}
	for _, f := range cs {
		m.Files = append(m.Files, File{Name: f.name, FirstHeight: f.first, LastHeight: f.last})
	}
	sort.Strings(extras)
	for _, n := range extras {
		m.Files = append(m.Files, File{Name: n})
	}
	err = forEachFile(len(m.Files), o.Workers, func(i int) error {
		size, sum, err := hashFile(filepath.Join(dir, m.Files[i].Name), -1)
		if err != nil {
			return err
		}
		m.Files[i].Size, m.Files[i].SHA256 = size, sum
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, f := range m.Files {
		m.TotalBytes += f.Size
	}
	return m, nil
}

// Marshal returns the canonical manifest bytes (the bytes that are signed).
func (m *Manifest) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Parse decodes and validates manifest bytes. It does not check a signature.
func Parse(b []byte) (*Manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if dec.More() {
		return nil, errors.New("decode manifest: trailing data")
	}
	if m.Version != FormatVersion {
		return nil, fmt.Errorf("unsupported manifest version %d", m.Version)
	}
	if m.ChainID == "" || m.LastHeight < 1 || m.FirstHeight < 1 || m.FirstHeight > m.LastHeight {
		return nil, errors.New("manifest: invalid chain id or height range")
	}
	if err := checkHash32(m.LastAppHash); err != nil {
		return nil, fmt.Errorf("manifest last_app_hash: %w", err)
	}
	if len(m.Files) == 0 || len(m.Files) > maxFiles {
		return nil, fmt.Errorf("manifest: %d files", len(m.Files))
	}
	seen := map[string]bool{}
	var total int64
	for _, f := range m.Files {
		if err := checkName(f.Name); err != nil {
			return nil, fmt.Errorf("manifest: %w", err)
		}
		if seen[f.Name] {
			return nil, fmt.Errorf("manifest: duplicate file %s", f.Name)
		}
		seen[f.Name] = true
		if f.Size < 0 || total > (1<<62)-f.Size {
			return nil, fmt.Errorf("manifest: bad size for %s", f.Name)
		}
		total += f.Size
		if sum, err := hex.DecodeString(f.SHA256); err != nil || len(sum) != sha256.Size {
			return nil, fmt.Errorf("manifest: bad sha256 for %s", f.Name)
		}
		if _, _, isCS := parseChangesetName(f.Name); isCS {
			if f.FirstHeight < m.FirstHeight || f.LastHeight < f.FirstHeight || f.LastHeight > m.LastHeight {
				return nil, fmt.Errorf("manifest: bad height range for %s", f.Name)
			}
		}
	}
	if total != m.TotalBytes {
		return nil, fmt.Errorf("manifest: total_bytes %d != sum of sizes %d", m.TotalBytes, total)
	}
	return &m, nil
}

// VerifyOptions configures Verify.
type VerifyOptions struct {
	ChainID    string   // if set, the manifest's chain id must equal it
	Ignore     []string // directory entries that are neither listed nor rejected (e.g. the manifest itself)
	AllowExtra bool     // tolerate unlisted files instead of failing
	Workers    int
}

// Verify checks sig over manifestBytes with pub, then checks that dir holds exactly the files of the
// manifest with the listed sizes and sha256 sums. Hashing is streaming (bounded memory per worker).
func Verify(dir string, manifestBytes, sig []byte, pub ed25519.PublicKey, o VerifyOptions) (*Manifest, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, errors.New("bad public key")
	}
	if !ed25519.Verify(pub, manifestBytes, sig) {
		return nil, errors.New("SIGNATURE INVALID: manifest was not signed by this key or was modified")
	}
	m, err := Parse(manifestBytes)
	if err != nil {
		return nil, err
	}
	if o.ChainID != "" && o.ChainID != m.ChainID {
		return nil, fmt.Errorf("manifest is for chain %q, expected %q", m.ChainID, o.ChainID)
	}
	listed := map[string]int{}
	for i, f := range m.Files {
		listed[f.Name] = i
	}
	ignore := setOf(o.Ignore)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, e := range ents {
		name := e.Name()
		if _, ok := listed[name]; !ok {
			if ignore[name] || o.AllowExtra {
				continue
			}
			return nil, fmt.Errorf("%s: unexpected file in bundle directory (not in manifest)", name)
		}
		if !e.Type().IsRegular() {
			return nil, fmt.Errorf("%s: not a regular file", name)
		}
		present[name] = true
	}
	var missing []string
	for _, f := range m.Files {
		if !present[f.Name] {
			missing = append(missing, f.Name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%d file(s) missing, first: %s", len(missing), missing[0])
	}
	// cheap size check first, so a truncated download fails before hours of hashing
	for _, f := range m.Files {
		st, err := os.Lstat(filepath.Join(dir, f.Name))
		if err != nil {
			return nil, err
		}
		if st.Size() != f.Size {
			return nil, fmt.Errorf("%s: size %d, manifest says %d", f.Name, st.Size(), f.Size)
		}
	}
	err = forEachFile(len(m.Files), o.Workers, func(i int) error {
		f := m.Files[i]
		_, sum, err := hashFile(filepath.Join(dir, f.Name), f.Size)
		if err != nil {
			return err
		}
		if !strings.EqualFold(sum, f.SHA256) {
			return fmt.Errorf("%s: sha256 %s, manifest says %s", f.Name, sum, f.SHA256)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// hashFile streams the file through sha256. It refuses non-regular files and, if want >= 0,
// reads at most want+1 bytes and fails unless exactly want bytes were read.
func hashFile(p string, want int64) (int64, string, error) {
	st, err := os.Lstat(p)
	if err != nil {
		return 0, "", err
	}
	if !st.Mode().IsRegular() {
		return 0, "", fmt.Errorf("%s: not a regular file", p)
	}
	f, err := os.Open(p)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	var r io.Reader = f
	if want >= 0 {
		r = io.LimitReader(f, want+1)
	}
	n, err := io.CopyBuffer(h, r, make([]byte, 1<<20))
	if err != nil {
		return 0, "", fmt.Errorf("%s: %w", p, err)
	}
	if want >= 0 && n != want {
		return 0, "", fmt.Errorf("%s: read %d bytes, manifest says %d", filepath.Base(p), n, want)
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// forEachFile runs fn(0..n-1) on up to workers goroutines and returns the first error.
func forEachFile(n, workers int, fn func(int) error) error {
	workers = max(1, min(workers, n))
	idx := make(chan int)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	var once sync.Once
	stop := make(chan struct{})
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				if err := fn(i); err != nil {
					errs <- err
					once.Do(func() { close(stop) })
					return
				}
			}
		}()
	}
feed:
	for i := range n {
		select {
		case idx <- i:
		case <-stop:
			break feed
		}
	}
	close(idx)
	wg.Wait()
	close(errs)
	return <-errs
}

func checkHash32(h string) error {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != 32 {
		return fmt.Errorf("want 64 hex characters, got %q", h)
	}
	return nil
}

func setOf(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// ReadLimited reads at most limit bytes of p and fails if the file is larger.
func ReadLimited(p string, limit int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: larger than %d bytes", p, limit)
	}
	return b, nil
}
