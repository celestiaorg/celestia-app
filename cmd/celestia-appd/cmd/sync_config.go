package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/creachadair/tomledit"
	"github.com/creachadair/tomledit/parser"
	"github.com/creachadair/tomledit/transform"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// configFile is a config file that sync can extend with the binary's defaults.
type configFile struct {
	path      string
	reference []byte
}

func syncConfigCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use: "sync", Short: "Add missing config.toml and Fibre server_config.toml settings and their documentation", Args: cobra.NoArgs,
		// Skip the root loader, which can create files even for a dry run.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, _ []string) error {
			executable, err := os.Executable()
			if err != nil {
				return err
			}
			v := viper.New()
			v.SetEnvPrefix(filepath.Base(executable))
			v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
			v.AutomaticEnv()
			if err := v.BindPFlag(flags.FlagHome, cmd.Flags().Lookup(flags.FlagHome)); err != nil {
				return err
			}
			fibreHome, err := cmd.Flags().GetString(flagFibreHome)
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed(flagFibreHome) && os.Getenv(fibre.EnvHome) != "" {
				fibreHome = os.Getenv(fibre.EnvHome)
			}
			consensus, err := renderConsensusConfig()
			if err != nil {
				return err
			}
			fibreReference, err := renderFibreConfig()
			if err != nil {
				return err
			}
			home := v.GetString(flags.FlagHome)
			files := []configFile{
				{path: filepath.Join(home, "config", "config.toml"), reference: consensus},
				{path: fibre.DefaultConfigPath(fibreHome), reference: fibreReference},
			}
			// Hosts that run only the node or only Fibre have just one of the files.
			var present []configFile
			for _, file := range files {
				if _, err := os.Lstat(file.path); os.IsNotExist(err) {
					cmd.Printf("%s not found at %s, skipping\n", filepath.Base(file.path), file.path)
					continue
				}
				// Validate before writing anything, so a bad file leaves the others untouched.
				if _, _, err := syncConfigFile(file.path, file.reference, true); err != nil {
					return err
				}
				present = append(present, file)
			}
			if len(present) == 0 {
				return fmt.Errorf("no config files found")
			}
			for _, file := range present {
				name := filepath.Base(file.path)
				added, backup, err := syncConfigFile(file.path, file.reference, dryRun)
				if err != nil {
					return err
				}
				switch {
				case len(added) == 0:
					cmd.Printf("%s is up to date\n", name)
				case dryRun:
					cmd.Printf("Settings to add to %s: %s\n", name, strings.Join(added, ", "))
				default:
					cmd.Printf("Added settings to %s: %s\nBackup: %s\n", name, strings.Join(added, ", "), backup)
				}
			}
			return nil
		},
	}
	cmd.Flags().String(flags.FlagHome, app.NodeHome, "The application home directory")
	cmd.Flags().String(flagFibreHome, fibre.DefaultHome(), fmt.Sprintf("The Fibre home directory (or set %s)", fibre.EnvHome))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show missing settings without modifying files")
	return cmd
}

const flagFibreHome = "fibre-home"

func renderConsensusConfig() ([]byte, error) {
	var reference bytes.Buffer
	err := cmtcfg.RenderConfig(&reference, app.DefaultConsensusConfig())
	return reference.Bytes(), err
}

// renderFibreConfig renders server_config.toml as the Fibre server saves it.
func renderFibreConfig() ([]byte, error) {
	return toml.Marshal(fibre.DefaultServerConfig())
}

// mergeConfig adds missing documented settings while preserving existing values and comments.
func mergeConfig(original, reference []byte) ([]byte, []string, error) {
	var before map[string]any
	if err := toml.Unmarshal(original, &before); err != nil {
		return nil, nil, err
	}
	current, err := tomledit.Parse(bytes.NewReader(original))
	if err != nil {
		return nil, nil, err
	}
	template, err := tomledit.Parse(bytes.NewReader(reference))
	if err != nil {
		return nil, nil, err
	}
	present := make(map[string]bool)
	current.Scan(func(key parser.Key, _ *tomledit.Entry) bool {
		present[strings.ToLower(key.String())] = true
		return true
	})
	var added []string
	for _, section := range append([]*tomledit.Section{template.Global}, template.Sections...) {
		if section == nil {
			continue
		}
		name := section.TableName()
		// The core template uses top-level settings and single-level sections.
		if len(name) > 1 || (section.Heading != nil && section.IsArray) {
			return nil, nil, fmt.Errorf("unsupported template section %s", name)
		}
		destination := current.Global
		dotted := false
		if len(name) == 1 {
			actualKey, exists, err := configKey(before, name[0])
			if err != nil {
				return nil, nil, err
			}
			if exists {
				name = parser.Key{actualKey}
				if table := transform.FindTable(current, name...); table != nil {
					destination = table.Section
				} else {
					dotted = true
				}
			} else {
				destination = &tomledit.Section{Heading: section.Heading}
				current.Sections = append(current.Sections, destination)
			}
		}
		for _, item := range section.Items {
			kv, ok := item.(*parser.KeyValue)
			if !ok {
				continue
			}
			full := append(append(parser.Key(nil), name...), kv.Name...)
			if present[strings.ToLower(full.String())] {
				continue
			}
			added = append(added, strings.Join(full, "."))
			copyKV := *kv
			if dotted {
				copyKV.Name = full
			}
			transform.InsertMapping(destination, &copyKV, false)
		}
	}
	if len(added) == 0 {
		return original, nil, nil
	}
	var result bytes.Buffer
	if err := tomledit.Format(&result, current); err != nil {
		return nil, nil, err
	}
	var actual map[string]any
	if err := toml.Unmarshal(result.Bytes(), &actual); err != nil {
		return nil, nil, fmt.Errorf("cannot safely extend config: %w", err)
	}
	if !configValuesPreserved(before, actual) {
		return nil, nil, fmt.Errorf("config update would change existing values")
	}
	return result.Bytes(), added, nil
}

// configValuesPreserved allows additions but rejects changes to existing values.
func configValuesPreserved(before, after map[string]any) bool {
	for key, value := range before {
		if table, ok := value.(map[string]any); ok {
			updated, ok := after[key].(map[string]any)
			if !ok || !configValuesPreserved(table, updated) {
				return false
			}
		} else if !reflect.DeepEqual(value, after[key]) {
			return false
		}
	}
	return true
}

// Match the SDK's case-insensitive keys without introducing ambiguous duplicates.
func configKey(values map[string]any, key string) (string, bool, error) {
	var actual string
	var found bool
	for candidate := range values {
		if strings.EqualFold(candidate, key) {
			if found {
				return "", false, fmt.Errorf("ambiguous config key %s", key)
			}
			actual, found = candidate, true
		}
	}
	return actual, found, nil
}

func syncConfigFile(path string, reference []byte, dryRun bool) ([]string, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 {
		return nil, "", fmt.Errorf("config must be a regular file without links: %s", path)
	}
	if info.Mode().Perm()&0o222 == 0 {
		return nil, "", fmt.Errorf("config is read-only: %s", path)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	updated, added, err := mergeConfig(original, reference)
	if err != nil || len(added) == 0 || dryRun {
		return added, "", err
	}
	backup, err := replaceConfigFile(path, original, updated, info)
	return added, backup, err
}

func replaceConfigFile(path string, original, updated []byte, info os.FileInfo) (string, error) {
	stat := info.Sys().(*syscall.Stat_t)
	temp, err := writeConfigTemp(path, ".tmp-*", updated, info)
	if err != nil {
		return "", err
	}
	defer os.Remove(temp)
	backup, err := writeConfigTemp(path, ".backup-*", original, info)
	if err != nil {
		return "", err
	}
	// Do not overwrite an operator edit made while preparing the update.
	current, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	currentStat, ok := current.Sys().(*syscall.Stat_t)
	if !ok || currentStat.Nlink != 1 || currentStat.Uid != stat.Uid || currentStat.Gid != stat.Gid ||
		!os.SameFile(info, current) || info.Mode() != current.Mode() || !info.ModTime().Equal(current.ModTime()) || !bytes.Equal(original, contents) {
		return "", fmt.Errorf("config changed while preparing update: %s", path)
	}
	if err := os.Rename(temp, path); err != nil {
		return "", err
	}
	return backup, nil
}

func writeConfigTemp(path, suffix string, contents []byte, info os.FileInfo) (name string, err error) {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+suffix)
	if err != nil {
		return "", err
	}
	name = f.Name()
	defer func() {
		if err != nil {
			os.Remove(name)
		}
	}()
	defer f.Close()
	stat := info.Sys().(*syscall.Stat_t)
	if err = f.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
		return name, err
	}
	if err = f.Chmod(info.Mode().Perm()); err != nil {
		return name, err
	}
	if _, err = f.Write(contents); err != nil {
		return name, err
	}
	if err = f.Sync(); err != nil {
		return name, err
	}
	return name, f.Close()
}
