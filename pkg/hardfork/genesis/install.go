package genesis

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

const (
	stepInstall = "install"
	tempPattern = ".from-export-*"
)

// Replaceable for failure injection in tests.
var (
	link    = os.Link
	remove  = os.Remove
	syncDir = func(dir string) error {
		f, err := os.Open(dir)
		if err != nil {
			return err
		}
		return errors.Join(f.Sync(), f.Close())
	}
)

// Install writes g to output and rep to report without replacing anything, and fills rep's hash and sizes.
// The report is linked first, so a genesis never exists without its report. On error, the paths this run
// created are removed, and the error names any that remain.
func Install(g *Genesis, rep *Report, input, output, report string) (err error) {
	output, report, err = installPaths(input, output, report)
	if err != nil {
		return err
	}
	outDir, repDir := filepath.Dir(output), filepath.Dir(report)
	var tmps, links []string
	defer func() {
		if err != nil {
			err = cleanup(err, links, tmps, outDir, repDir)
		}
	}()

	var w Written
	tmp, err := writeTemp(outDir, func(bw io.Writer) (err error) {
		w, err = WriteGenesis(bw, g)
		return err
	})
	if tmp != "" {
		tmps = append(tmps, tmp)
	}
	if err != nil {
		return err
	}
	got, err := hashFile(tmp)
	if err != nil {
		return err
	}
	if got != w.SHA256 {
		return &CheckError{Step: stepInstall, Field: "output", Want: hex.EncodeToString(w.SHA256[:]), Got: hex.EncodeToString(got[:])}
	}

	rep.GenesisSHA256 = hex.EncodeToString(w.SHA256[:])
	rep.Sizes = w.Sizes
	rep.Transport = transportNote(w.Sizes.InitChain)
	repTmp, err := writeTemp(repDir, func(bw io.Writer) error {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err == nil {
			_, err = bw.Write(append(b, '\n'))
		}
		return err
	})
	if repTmp != "" {
		tmps = append(tmps, repTmp)
	}
	if err != nil {
		return err
	}

	// links is kept newest first, so cleanup removes the genesis before its report.
	for _, s := range []struct{ tmp, dst, dir string }{{repTmp, report, repDir}, {tmp, output, outDir}} {
		if err := link(s.tmp, s.dst); err != nil {
			return err
		}
		links = slices.Insert(links, 0, s.dst)
		if err := syncDir(s.dir); err != nil {
			return err
		}
	}
	for len(tmps) > 0 {
		if err := remove(tmps[0]); err != nil {
			return err
		}
		tmps = tmps[1:]
	}
	if err := syncDir(outDir); err != nil {
		return err
	}
	return syncDir(repDir)
}

// installPaths resolves the parents of the three paths and requires distinct, absent destinations.
func installPaths(input, output, report string) (string, string, error) {
	var resolved [3]string
	for i, p := range []string{input, output, report} {
		abs, err := filepath.Abs(p)
		if err == nil {
			var dir string
			dir, err = filepath.EvalSymlinks(filepath.Dir(abs))
			resolved[i] = filepath.Join(dir, filepath.Base(abs))
		}
		if err != nil {
			return "", "", err
		}
	}
	in, out, rep := resolved[0], resolved[1], resolved[2]
	if in == out || in == rep || out == rep {
		return "", "", &CheckError{Step: stepInstall, Field: "paths", Want: "distinct input, output and report", Got: fmt.Sprintf("%s, %s, %s", in, out, rep)}
	}
	for _, p := range []string{out, rep} {
		// Lstat also sees a dangling symlink, which link(2) would not follow.
		_, err := os.Lstat(p)
		if err == nil {
			return "", "", &CheckError{Step: stepInstall, Field: p, Want: "absent", Got: "exists"}
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", "", err
		}
	}
	return out, rep, nil
}

// writeTemp writes a new temp file in dir and syncs it; the path is returned even on error.
func writeTemp(dir string, write func(io.Writer) error) (string, error) {
	f, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return "", err
	}
	bw := bufio.NewWriter(f)
	err = write(bw)
	if err == nil {
		err = bw.Flush()
	}
	if err == nil {
		err = f.Sync()
	}
	return f.Name(), errors.Join(err, f.Close())
}

// cleanup removes links in order, then tmps, and syncs dirs; cause is returned joined with what could not be
// cleaned up. A link is removed only once the one before it is removed and its dir synced, so neither a failed
// removal nor a crash leaves a genesis without its report.
func cleanup(cause error, links, tmps []string, dirs ...string) error {
	errs := []error{cause}
	for i, p := range links {
		err := remove(p)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			err = syncDir(filepath.Dir(p))
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("leftover %s: %w", p, err))
			for _, kept := range links[i+1:] {
				errs = append(errs, fmt.Errorf("leftover %s: kept because %s may remain", kept, p))
			}
			break
		}
	}
	for _, p := range tmps {
		if err := remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("leftover %s: %w", p, err))
		}
	}
	for _, d := range dirs {
		if err := syncDir(d); err != nil {
			errs = append(errs, fmt.Errorf("sync %s: %w", d, err))
		}
	}
	return errors.Join(errs...)
}
