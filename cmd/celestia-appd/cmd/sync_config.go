package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"cosmossdk.io/log"
	confixcmd "cosmossdk.io/tools/confix/cmd"
	"github.com/celestiaorg/celestia-app/v10/app"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/creachadair/tomledit"
	"github.com/creachadair/tomledit/parser"
	"github.com/creachadair/tomledit/transform"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newConfigRootCmd() *cobra.Command {
	root := &cobra.Command{Use: "celestia-appd", SilenceUsage: true}
	root.PersistentFlags().String(FlagLogToFile, "", "Write logs directly to a file. If empty, logs are written to stderr")
	config := confixcmd.ConfigCommand()
	config.AddCommand(syncConfigCmd())
	root.AddCommand(config)
	return root
}

func syncConfigCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use: "sync", Short: "Add missing config.toml settings and their documentation", Args: cobra.NoArgs,
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
			path := filepath.Join(v.GetString(flags.FlagHome), "config", "config.toml")
			added, backup, err := syncConfigFile(path, dryRun)
			if err != nil {
				return err
			}
			if len(added) == 0 {
				cmd.Println("config.toml is up to date")
				return nil
			}
			if dryRun {
				cmd.Printf("Settings to add:\n%s\n", strings.Join(added, "\n"))
			} else {
				cmd.Printf("Added settings:\n%s\nBackup: %s\n", strings.Join(added, "\n"), backup)
			}
			return nil
		},
	}
	cmd.Flags().String(flags.FlagHome, app.NodeHome, "The application home directory")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show missing settings without modifying files")
	return cmd
}

func syncConfigOnStart(cmd *cobra.Command, logger log.Logger) error {
	path := filepath.Join(server.GetServerContextFromCmd(cmd).Config.RootDir, "config", "config.toml")
	added, backup, err := syncConfigFile(path, false)
	if err != nil {
		logger.Warn("Could not add missing config settings; runtime defaults still apply", "path", path, "err", err)
	} else if len(added) > 0 {
		logger.Info("Added missing config settings", "settings", added, "backup", backup)
	}
	return nil
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
	entries := make(map[string]*tomledit.Entry)
	current.Scan(func(key parser.Key, entry *tomledit.Entry) bool {
		if (entry.IsSection() && (len(key) != 1 || entry.IsArray)) || (entry.IsMapping() && len(entry.Name) != 1) {
			err = fmt.Errorf("cannot extend %s: use ordinary [section] tables and undotted keys", key)
			return false
		}
		name := strings.ToLower(key.String())
		if entries[name] != nil {
			err = fmt.Errorf("ambiguous config key %s", key)
			return false
		}
		entries[name] = entry
		return true
	})
	if err != nil {
		return nil, nil, err
	}
	var added []string
	template.Scan(func(key parser.Key, entry *tomledit.Entry) bool {
		if entry.IsSection() || entries[strings.ToLower(key.String())] != nil {
			return true
		}
		name := entry.TableName()
		if len(name) > 1 || entry.IsInline() || (entry.Heading != nil && entry.IsArray) {
			err = fmt.Errorf("unsupported template section %s", name)
			return false
		}
		destination := current.Global
		if len(name) == 1 {
			sectionKey := strings.ToLower(name.String())
			if existing := entries[sectionKey]; existing != nil {
				if !existing.IsSection() {
					err = fmt.Errorf("cannot extend %s: use an ordinary [section] table", name)
					return false
				}
				destination = existing.Section
			} else {
				destination = &tomledit.Section{Heading: entry.Heading}
				current.Sections = append(current.Sections, destination)
				entries[sectionKey] = &tomledit.Entry{Section: destination}
			}
		}
		transform.InsertMapping(destination, entry.KeyValue, false)
		added = append(added, fmt.Sprintf("%s = %s", key, entry.Value))
		return true
	})
	if err != nil {
		return nil, nil, err
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
		} else if !cmp.Equal(value, after[key], cmpopts.EquateNaNs()) {
			return false
		}
	}
	return true
}

func syncConfigFile(path string, dryRun bool) ([]string, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 {
		return nil, "", fmt.Errorf("config must be a regular file without links: %s", path)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var reference bytes.Buffer
	if err := cmtcfg.RenderConfig(&reference, app.DefaultConsensusConfig()); err != nil {
		return nil, "", err
	}
	updated, added, err := mergeConfig(original, reference.Bytes())
	if err != nil || len(added) == 0 || dryRun {
		return added, "", err
	}
	backup, err := replaceConfigFile(path, original, updated, info)
	return added, backup, err
}

func replaceConfigFile(path string, original, updated []byte, info os.FileInfo) (string, error) {
	stat := info.Sys().(*syscall.Stat_t)
	if info.Mode().Perm()&0o222 == 0 {
		return "", fmt.Errorf("config is read-only: %s", path)
	}
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
