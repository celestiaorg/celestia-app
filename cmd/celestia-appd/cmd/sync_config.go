package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v10/app"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/creachadair/tomledit"
	"github.com/creachadair/tomledit/parser"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

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
				cmd.Printf("Settings to add: %s\n", strings.Join(added, ", "))
			} else {
				cmd.Printf("Added settings: %s\nBackup: %s\n", strings.Join(added, ", "), backup)
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
		logger.Info("Added missing config settings", "keys", added, "backup", backup)
	}
	return nil
}

// mergeConfig inserts missing documented settings without rewriting existing text.
func mergeConfig(original, reference []byte) ([]byte, []string, error) {
	var expected, defaults map[string]any
	if err := toml.Unmarshal(original, &expected); err != nil {
		return nil, nil, err
	}
	if err := toml.Unmarshal(reference, &defaults); err != nil {
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
	lines := strings.SplitAfter(string(original), "\n")
	insertions := make(map[int]string)
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
		target, source := expected, defaults
		at, dotted := 0, false
		var heading *parser.Heading
		if len(name) == 1 {
			source = defaults[name[0]].(map[string]any)
			actualKey, exists, err := configKey(expected, name[0])
			if err != nil {
				return nil, nil, err
			}
			if exists {
				name = parser.Key{actualKey}
				var ok bool
				target, ok = expected[actualKey].(map[string]any)
				if !ok {
					return nil, nil, fmt.Errorf("%s is not a table", name)
				}
				dotted = true
				for _, s := range current.Sections {
					if s.TableName().Equals(name) && !s.IsArray {
						at, dotted = s.Line, false
						break
					}
				}
			} else {
				target = make(map[string]any)
				expected[name[0]] = target
				at, heading = len(lines), section.Heading
			}
		}
		var items []parser.Item
		for _, item := range section.Items {
			kv, ok := item.(*parser.KeyValue)
			if !ok {
				continue
			}
			if len(kv.Name) != 1 {
				return nil, nil, fmt.Errorf("unsupported template key %s", kv.Name)
			}
			key := kv.Name[0]
			_, exists, err := configKey(target, key)
			if err != nil {
				return nil, nil, err
			}
			if exists {
				continue
			}
			target[key] = source[key]
			full := append(append(parser.Key(nil), name...), key)
			added = append(added, strings.Join(full, "."))
			copyKV := *kv
			if dotted {
				copyKV.Name = full
			}
			items = append(items, &copyKV)
		}
		if len(items) == 0 {
			continue
		}
		doc := &tomledit.Document{Global: &tomledit.Section{Items: items}}
		if heading != nil {
			doc = &tomledit.Document{Sections: []*tomledit.Section{{Heading: heading, Items: items}}}
		}
		var rendered bytes.Buffer
		if err := tomledit.Format(&rendered, doc); err != nil {
			return nil, nil, err
		}
		insertions[at] += "\n" + rendered.String() + "\n"
	}
	if len(added) == 0 {
		return original, nil, nil
	}
	var result bytes.Buffer
	for i, line := range lines {
		result.WriteString(insertions[i])
		result.WriteString(line)
	}
	result.WriteString(insertions[len(lines)])
	var actual map[string]any
	if err := toml.Unmarshal(result.Bytes(), &actual); err != nil {
		return nil, nil, fmt.Errorf("cannot safely extend config: %w", err)
	}
	if !reflect.DeepEqual(expected, actual) {
		return nil, nil, fmt.Errorf("config update would change existing values")
	}
	return result.Bytes(), added, nil
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
