package cmd

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v9/app"
	cmtdbm "github.com/cometbft/cometbft-db"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	"github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/spf13/cobra"
)

const (
	flagHeight             = "height"
	flagOutput             = "output"
	flagLogLines           = "log-lines"
	flagLogFile            = "log-file"
	flagSkipAppDB          = "skip-app-db"
	flagSkipCometDB        = "skip-comet-db"
	flagIncludeValidatorSt = "include-validator-state"
	defaultLogLines        = 5000
)

// sensitiveKeySubstrings lists case-insensitive substrings of TOML keys whose
// values are blanked out when redacting config files. It covers obvious secrets
// and peer/address information that operators may not want to share publicly.
var sensitiveKeySubstrings = []string{
	"secret", "password", "passwd", "token", "mnemonic", "private_key", "priv_key",
	"persistent_peers", "seeds", "external_address", "private_peer_ids",
	"unconditional_peer_ids", "bootstrap",
}

// AppHashMismatchCmd returns the parent `app-hash-mismatch` debug command.
func AppHashMismatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app-hash-mismatch",
		Short: "Tools for diagnosing app hash mismatches",
		Long: `Tools for diagnosing app hash mismatches (consensus failures where this
node computed a different AppHash than the rest of the network).

IMPORTANT: An app hash mismatch destroys nothing on its own, but a rollback or
resync does. Before you roll back, preserve the evidence:

  1. Stop the node.
  2. Copy $HOME/data to a safe location.
  3. Run "app-hash-mismatch collect" against the frozen copy.
  4. Only then roll back or resync.`,
	}
	cmd.AddCommand(
		appHashMismatchCollectCmd(),
		appHashMismatchRunBlockCmd(),
	)
	return cmd
}

func appHashMismatchCollectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collect",
		Short: "Collect app hash mismatch diagnostics into a shareable archive",
		Long: `Collect diagnostic data for an app hash mismatch into a single tar.gz archive.

The mismatch is detected at height H, but the divergence actually happened while
executing H-1, so this command gathers data around H-2, H-1 and H.

The archive is safe to share: private validator and node keys are never included,
and config files are redacted. Verify integrity with the included SHA256SUMS file.

Example:
  celestia-appd debug app-hash-mismatch collect \
    --home ~/.celestia-app \
    --height 11729987 \
    --output apphash-debug-11729987.tar.gz`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			serverCtx := server.GetServerContextFromCmd(cmd)

			height, err := cmd.Flags().GetInt64(flagHeight)
			if err != nil {
				return err
			}
			output, err := cmd.Flags().GetString(flagOutput)
			if err != nil {
				return err
			}
			logLines, err := cmd.Flags().GetInt(flagLogLines)
			if err != nil {
				return err
			}
			logFile, err := cmd.Flags().GetString(flagLogFile)
			if err != nil {
				return err
			}
			skipAppDB, err := cmd.Flags().GetBool(flagSkipAppDB)
			if err != nil {
				return err
			}
			skipCometDB, err := cmd.Flags().GetBool(flagSkipCometDB)
			if err != nil {
				return err
			}
			includeValidatorState, err := cmd.Flags().GetBool(flagIncludeValidatorSt)
			if err != nil {
				return err
			}

			c := &collector{
				cmd:                   cmd,
				serverCtx:             serverCtx,
				home:                  serverCtx.Config.RootDir,
				height:                height,
				output:                output,
				logLines:              logLines,
				logFile:               logFile,
				skipAppDB:             skipAppDB,
				skipCometDB:           skipCometDB,
				includeValidatorState: includeValidatorState,
			}
			return c.run()
		},
	}

	cmd.Flags().String(flags.FlagHome, app.NodeHome, "node home directory (run against a frozen copy of data/ if possible)")
	cmd.Flags().Int64(flagHeight, 0, "mismatch height H (defaults to the comet state's last block height + 1)")
	cmd.Flags().String(flagOutput, "", "output archive path (defaults to apphash-debug-<H>.tar.gz in the current directory)")
	cmd.Flags().Int(flagLogLines, defaultLogLines, "number of trailing log lines to include from --log-file")
	cmd.Flags().String(flagLogFile, "", "path to a node log file to tail into the archive")
	cmd.Flags().Bool(flagSkipAppDB, false, "do not copy data/application.db into the archive")
	cmd.Flags().Bool(flagSkipCometDB, false, "do not copy data/state.db and data/blockstore.db into the archive")
	cmd.Flags().Bool(flagIncludeValidatorSt, false, "include data/priv_validator_state.json (SENSITIVE; never includes private keys)")

	return cmd
}

type collector struct {
	cmd                   *cobra.Command
	serverCtx             *server.Context
	home                  string
	height                int64
	output                string
	logLines              int
	logFile               string
	skipAppDB             bool
	skipCometDB           bool
	includeValidatorState bool

	warnings []string
}

func (c *collector) warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	c.warnings = append(c.warnings, msg)
	fmt.Fprintf(c.cmd.ErrOrStderr(), "warning: %s\n", msg)
}

func (c *collector) run() error {
	dataDir := filepath.Join(c.home, "data")
	configDir := filepath.Join(c.home, "config")
	collectedAt := time.Now().UTC()

	// Open the application DB (cosmos-db) and the CometBFT block/state stores
	// (cometbft-db is a different DB interface from cosmos-db).
	appBackend := server.GetAppDBBackend(c.serverCtx.Viper)
	appDB, err := dbm.NewDB("application", appBackend, dataDir)
	if err != nil {
		return fmt.Errorf("open application db: %w", err)
	}
	defer appDB.Close()

	cmtBackend := cmtdbm.BackendType(c.serverCtx.Config.DBBackend)
	blockDB, err := cmtdbm.NewDB("blockstore", cmtBackend, dataDir)
	if err != nil {
		return fmt.Errorf("open blockstore db: %w", err)
	}
	defer blockDB.Close()
	stateDB, err := cmtdbm.NewDB("state", cmtBackend, dataDir)
	if err != nil {
		return fmt.Errorf("open state db: %w", err)
	}
	defer stateDB.Close()

	blockStore := store.NewBlockStore(blockDB)
	stateStore := sm.NewStore(stateDB, sm.StoreOptions{DiscardABCIResponses: false})

	cometState, stateErr := stateStore.Load()
	if stateErr != nil {
		c.warn("failed to load comet state: %v", stateErr)
	}

	// Resolve the mismatch height: default to the height that would have failed
	// (last committed comet height + 1).
	if c.height == 0 {
		if stateErr != nil || cometState.LastBlockHeight == 0 {
			return fmt.Errorf("could not auto-detect height; pass --height explicitly")
		}
		c.height = cometState.LastBlockHeight + 1
		fmt.Fprintf(c.cmd.OutOrStdout(), "no --height given; defaulting to %d (comet last block + 1)\n", c.height)
	}

	if c.output == "" {
		c.output = fmt.Sprintf("apphash-debug-%d.tar.gz", c.height)
	}

	// Read the application multistore commit info (height + per-version hashes).
	appInfo := c.readAppStoreInfo(appDB)

	// Create the archive.
	outFile, err := os.Create(c.output)
	if err != nil {
		return fmt.Errorf("create output %q: %w", c.output, err)
	}
	defer outFile.Close()
	gz := gzip.NewWriter(outFile)
	tw := tar.NewWriter(gz)
	aw := &archiveWriter{tw: tw, modTime: collectedAt}

	// block_H.pb, block_H-1.pb, block_H-2.pb
	for _, h := range []int64{c.height, c.height - 1, c.height - 2} {
		c.addBlock(aw, blockStore, h)
	}

	// finalize_response_H-1
	c.addFinalizeResponse(aw, stateStore, c.height-1)

	// store_hashes.json at H-2, H-1 and current app height
	storeHashes := c.buildStoreHashes(appInfo)
	c.addJSON(aw, "store_hashes.json", storeHashes)

	// Redacted config files.
	for _, name := range []string{"config.toml", "app.toml", "client.toml"} {
		c.addRedactedConfig(aw, filepath.Join(configDir, name), filepath.Join("config", name))
	}

	// Node logs (tail).
	c.addLogs(aw)

	// priv_validator_state.json (opt-in, never the keys).
	if c.includeValidatorState {
		fmt.Fprintln(c.cmd.ErrOrStderr(), "SENSITIVE: including priv_validator_state.json as requested (private keys are NEVER included)")
		c.addFileIfExists(aw, filepath.Join(dataDir, "priv_validator_state.json"), "data/priv_validator_state.json")
	}

	// Full DB copies (largest entries — added after the cheap stuff).
	if !c.skipAppDB {
		c.addDir(aw, "data/application.db", filepath.Join(dataDir, "application.db"))
	}
	if !c.skipCometDB {
		c.addDir(aw, "data/state.db", filepath.Join(dataDir, "state.db"))
		c.addDir(aw, "data/blockstore.db", filepath.Join(dataDir, "blockstore.db"))
	}

	// manifest.json and mismatch.json (built last so they can record warnings).
	manifest := c.buildManifest(collectedAt, cometState.ChainID)
	c.addJSON(aw, "manifest.json", manifest)
	mismatch := c.buildMismatch(blockStore, cometState, appInfo, stateErr)
	c.addJSON(aw, "mismatch.json", mismatch)

	// SHA256SUMS must be written last so it covers every other entry.
	if err := aw.writeSums("SHA256SUMS"); err != nil {
		return fmt.Errorf("write SHA256SUMS: %w", err)
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("close gzip: %w", err)
	}
	if err := outFile.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}

	info, err := os.Stat(c.output)
	if err != nil {
		return err
	}
	out := c.cmd.OutOrStdout()
	fmt.Fprintf(out, "\nWrote %s (%s)\n", c.output, humanizeBytes(info.Size()))
	fmt.Fprintln(out, "Do not include private keys. Verify with SHA256SUMS before sharing, then send the archive to the Celestia team.")
	if len(c.warnings) > 0 {
		fmt.Fprintf(out, "Completed with %d warning(s); see collection_warnings in manifest.json.\n", len(c.warnings))
	}
	return nil
}

// appStoreInfo holds the data read from the application commit multistore.
type appStoreInfo struct {
	latestHeight int64
	latestHash   []byte
	// commitInfos maps a height to its CommitInfo (nil if unavailable).
	commitInfos map[int64]*storetypes.CommitInfo
}

func (c *collector) readAppStoreInfo(appDB dbm.DB) (info *appStoreInfo) {
	info = &appStoreInfo{commitInfos: map[int64]*storetypes.CommitInfo{}}

	// Building the full app can panic (e.g. missing/invalid app.toml options).
	// App store hashes are a nice-to-have, so degrade to a warning rather than
	// aborting the whole collection.
	defer func() {
		if r := recover(); r != nil {
			c.warn("failed to read application store info: %v", r)
		}
	}()

	appInstance := NewAppServer(c.serverCtx.Logger, appDB, nil, c.serverCtx.Viper)
	capp, ok := appInstance.(*app.App)
	if !ok {
		c.warn("could not cast application to *app.App; skipping app store info")
		return info
	}
	cms := capp.CommitMultiStore()
	if cms == nil {
		c.warn("application commit multistore is nil")
		return info
	}
	if err := cms.LoadLatestVersion(); err != nil {
		c.warn("failed to load latest application version: %v", err)
		return info
	}
	info.latestHeight = cms.LatestVersion()
	info.latestHash = capp.LastCommitID().Hash

	getter, ok := cms.(interface {
		GetCommitInfo(int64) (*storetypes.CommitInfo, error)
	})
	if !ok {
		c.warn("commit multistore does not expose GetCommitInfo; per-height hashes unavailable")
		return info
	}
	for _, h := range []int64{c.height - 2, c.height - 1, info.latestHeight} {
		if h <= 0 {
			continue
		}
		ci, err := getter.GetCommitInfo(h)
		if err != nil {
			c.warn("could not get app commit info for height %d: %v", h, err)
			continue
		}
		info.commitInfos[h] = ci
	}
	return info
}

func (c *collector) addBlock(aw *archiveWriter, bs *store.BlockStore, height int64) {
	if height <= 0 {
		return
	}
	if height < bs.Base() {
		c.warn("block %d is below blockstore base %d; skipping", height, bs.Base())
		return
	}
	// LoadBlock can panic on corrupt block data; degrade to a warning so a single
	// bad block does not abort the whole collection.
	defer func() {
		if r := recover(); r != nil {
			c.warn("failed to load block %d: %v", height, r)
		}
	}()
	block := bs.LoadBlock(height)
	if block == nil {
		c.warn("block %d not found in blockstore; skipping", height)
		return
	}
	pb, err := block.ToProto()
	if err != nil {
		c.warn("failed to convert block %d to proto: %v", height, err)
		return
	}
	bz, err := pb.Marshal()
	if err != nil {
		c.warn("failed to marshal block %d: %v", height, err)
		return
	}
	c.addBytes(aw, fmt.Sprintf("block_%d.pb", height), bz)
}

func (c *collector) addFinalizeResponse(aw *archiveWriter, ss sm.Store, height int64) {
	if height <= 0 {
		return
	}
	resp, err := ss.LoadFinalizeBlockResponse(height)
	if err != nil {
		c.warn("finalize block response for height %d unavailable (ABCI responses may be discarded): %v", height, err)
		return
	}
	bz, err := resp.Marshal()
	if err != nil {
		c.warn("failed to marshal finalize response for height %d: %v", height, err)
	} else {
		c.addBytes(aw, fmt.Sprintf("finalize_response_%d.pb", height), bz)
	}
	if jsonBz, err := json.MarshalIndent(resp, "", "  "); err == nil {
		c.addBytes(aw, fmt.Sprintf("finalize_response_%d.json", height), jsonBz)
	}
}

func (c *collector) buildStoreHashes(info *appStoreInfo) map[string]any {
	out := map[string]any{}
	heights := map[string]int64{
		"H-2":            c.height - 2,
		"H-1":            c.height - 1,
		"app_db_current": info.latestHeight,
	}
	for label, h := range heights {
		entry := map[string]any{"height": h}
		if ci, ok := info.commitInfos[h]; ok && ci != nil {
			entry["commit_hash"] = fmt.Sprintf("%X", ci.Hash())
			stores := map[string]string{}
			for _, si := range ci.StoreInfos {
				stores[si.Name] = fmt.Sprintf("%X", si.CommitId.Hash)
			}
			entry["stores"] = stores
		}
		out[label] = entry
	}
	return out
}

func (c *collector) buildManifest(collectedAt time.Time, chainID string) map[string]any {
	vi := version.NewInfo()
	return map[string]any{
		"name":                vi.Name,
		"app_name":            vi.AppName,
		"version":             vi.Version,
		"git_commit":          vi.GitCommit,
		"build_tags":          vi.BuildTags,
		"go_version":          vi.GoVersion,
		"os":                  runtime.GOOS,
		"arch":                runtime.GOARCH,
		"chain_id":            chainID,
		"mismatch_height":     c.height,
		"collected_at":        collectedAt.Format(time.RFC3339),
		"command_args":        os.Args,
		"collection_warnings": c.warnings,
	}
}

func (c *collector) buildMismatch(bs *store.BlockStore, cometState sm.State, info *appStoreInfo, stateErr error) map[string]any {
	out := map[string]any{
		"height": c.height,
	}
	if stateErr == nil {
		out["comet_last_block_height"] = cometState.LastBlockHeight
		out["comet_app_hash"] = fmt.Sprintf("%X", cometState.AppHash)
	}
	out["app_db_height"] = info.latestHeight
	out["app_db_hash"] = fmt.Sprintf("%X", info.latestHash)

	// The header's AppHash at height H is what the network expected; comparing it
	// to the locally computed comet AppHash reveals the divergence.
	if meta := c.loadBlockMeta(bs, c.height); meta != nil {
		out["got_app_hash_in_block_H"] = fmt.Sprintf("%X", meta.Header.AppHash)
		if stateErr == nil {
			out["expected_app_hash_local"] = fmt.Sprintf("%X", cometState.AppHash)
			out["matches"] = string(meta.Header.AppHash) == string(cometState.AppHash)
		}
	}
	return out
}

// loadBlockMeta loads a block meta, recovering from panics on corrupt data.
func (c *collector) loadBlockMeta(bs *store.BlockStore, height int64) (meta *types.BlockMeta) {
	defer func() {
		if r := recover(); r != nil {
			c.warn("failed to load block meta %d: %v", height, r)
			meta = nil
		}
	}()
	return bs.LoadBlockMeta(height)
}

func (c *collector) addRedactedConfig(aw *archiveWriter, srcPath, archiveName string) {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		c.warn("could not read %s: %v", srcPath, err)
		return
	}
	c.addBytes(aw, archiveName, redactTOML(data))
}

func (c *collector) addLogs(aw *archiveWriter) {
	if c.logFile == "" {
		c.warn("no --log-file provided; node logs not included (attach the last lines of your node log manually)")
		return
	}
	lines, err := tailLines(c.logFile, c.logLines)
	if err != nil {
		c.warn("could not tail log file %q: %v", c.logFile, err)
		return
	}
	c.addBytes(aw, "node_logs.txt", []byte(strings.Join(lines, "\n")+"\n"))
}

func (c *collector) addFileIfExists(aw *archiveWriter, srcPath, archiveName string) {
	if _, err := os.Stat(srcPath); err != nil {
		c.warn("could not stat %s: %v", srcPath, err)
		return
	}
	if err := aw.addFile(archiveName, srcPath); err != nil {
		c.warn("could not add %s: %v", srcPath, err)
	}
}

func (c *collector) addDir(aw *archiveWriter, archivePrefix, srcDir string) {
	size, err := dirSize(srcDir)
	if err != nil {
		c.warn("could not stat directory %s: %v", srcDir, err)
		return
	}
	fmt.Fprintf(c.cmd.OutOrStdout(), "archiving %s (%s)...\n", srcDir, humanizeBytes(size))
	if err := aw.addDir(archivePrefix, srcDir); err != nil {
		c.warn("error archiving %s: %v", srcDir, err)
	}
}

func (c *collector) addBytes(aw *archiveWriter, name string, data []byte) {
	if err := aw.addBytes(name, data); err != nil {
		c.warn("could not add %s: %v", name, err)
	}
}

func (c *collector) addJSON(aw *archiveWriter, name string, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		c.warn("could not marshal %s: %v", name, err)
		return
	}
	c.addBytes(aw, name, data)
}

// archiveWriter wraps a tar.Writer and accumulates SHA-256 sums of every entry.
type archiveWriter struct {
	tw      *tar.Writer
	modTime time.Time
	sums    []string // formatted "<hex>  <name>" lines
}

func (a *archiveWriter) record(name string, sum []byte) {
	a.sums = append(a.sums, fmt.Sprintf("%x  %s", sum, name))
}

func (a *archiveWriter) addBytes(name string, data []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o644,
		Size:    int64(len(data)),
		ModTime: a.modTime,
	}
	if err := a.tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := a.tw.Write(data); err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	a.record(name, sum[:])
	return nil
}

func (a *archiveWriter) addFile(name, srcPath string) error {
	info, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = name
	hdr.ModTime = a.modTime
	if err := a.tw.WriteHeader(hdr); err != nil {
		return err
	}
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(a.tw, hasher), f); err != nil {
		return err
	}
	a.record(name, hasher.Sum(nil))
	return nil
}

func (a *archiveWriter) addDir(archivePrefix, srcDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(srcDir, path)
		if rerr != nil {
			return rerr
		}
		name := filepath.ToSlash(filepath.Join(archivePrefix, rel))
		return a.addFile(name, path)
	})
}

func (a *archiveWriter) writeSums(name string) error {
	sort.Strings(a.sums)
	content := strings.Join(a.sums, "\n")
	if content != "" {
		content += "\n"
	}
	hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), ModTime: a.modTime}
	if err := a.tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := a.tw.Write([]byte(content))
	return err
}

// tomlKeyValue matches a "key = value" line, capturing leading whitespace, the
// key, the separator and the value.
var tomlKeyValue = regexp.MustCompile(`^(\s*)([A-Za-z0-9_.-]+)(\s*=\s*)(.+)$`)

// redactTOML blanks the values of keys matching sensitiveKeySubstrings while
// preserving file structure and comments.
func redactTOML(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		m := tomlKeyValue.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := strings.ToLower(m[2])
		for _, s := range sensitiveKeySubstrings {
			if strings.Contains(key, s) {
				lines[i] = m[1] + m[2] + m[3] + `"[REDACTED]"`
				break
			}
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// tailLines returns the last n lines of the file at path.
func tailLines(path string, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	const chunk = 64 * 1024
	var (
		buf      []byte
		pos      = info.Size()
		newlines int
	)
	for pos > 0 && newlines <= n {
		readSize := min(int64(chunk), pos)
		pos -= readSize
		tmp := make([]byte, readSize, readSize+int64(len(buf)))
		if _, err := f.ReadAt(tmp, pos); err != nil && err != io.EOF {
			return nil, err
		}
		buf = append(tmp, buf...)
		newlines = strings.Count(string(buf), "\n")
	}

	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		total += info.Size()
		return nil
	})
	return total, err
}

func humanizeBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
