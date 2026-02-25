package cmd

import (
	"fmt"
	"path/filepath"
	"strconv"

	iavlstore "cosmossdk.io/store/iavl"
	storetypes "cosmossdk.io/store/types"
	circuittypes "cosmossdk.io/x/circuit/types"
	evidencetypes "cosmossdk.io/x/evidence/types"
	"cosmossdk.io/x/feegrant"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v8/app"
	blobtypes "github.com/celestiaorg/celestia-app/v8/x/blob/types"
	minfeetypes "github.com/celestiaorg/celestia-app/v8/x/minfee/types"
	signaltypes "github.com/celestiaorg/celestia-app/v8/x/signal/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/server"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	packetforwardtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
	"github.com/spf13/cobra"
)

// CheckVersionCmd returns a command to check if a specific version exists in all IAVL stores
func CheckVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check-version [height]",
		Short: "Check if a specific block height version exists in all IAVL stores",
		Long: `Check if a specific block height version exists in all IAVL stores.
This is useful for debugging issues where queries fail with "version mismatch on immutable IAVL tree" errors.

Example:
  celestia-appd debug check-version 2748392
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			height, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid height: %w", err)
			}

			serverCtx := server.GetServerContextFromCmd(cmd)

			// Open database
			// The application database is located in <rootDir>/data/
			dbBackend := server.GetAppDBBackend(serverCtx.Viper)
			dataDir := filepath.Join(serverCtx.Config.RootDir, "data")
			db, err := dbm.NewDB("application", dbBackend, dataDir)
			if err != nil {
				return fmt.Errorf("failed to open database: %w", err)
			}
			defer db.Close()

			// Create app instance to access stores
			// serverCtx.Viper implements AppOptions interface
			appInstance := NewAppServer(serverCtx.Logger, db, nil, serverCtx.Viper)

			// Cast to *app.App to access app-specific methods
			capp, ok := appInstance.(*app.App)
			if !ok {
				return fmt.Errorf("failed to cast application to *app.App")
			}

			// Get the commit multi-store BEFORE calling LoadLatestVersion
			// This avoids the "baseapp already sealed" error
			cms := capp.CommitMultiStore()
			if cms == nil {
				return fmt.Errorf("commit multi-store is nil")
			}

			// Load the latest version directly on the commit multi-store
			// instead of through BaseApp.LoadLatestVersion() which seals the app
			if err := cms.LoadLatestVersion(); err != nil {
				return fmt.Errorf("failed to load latest version: %w", err)
			}

			// Get latest version
			latestVersion := cms.LatestVersion()
			fmt.Printf("Latest version: %d\n", latestVersion)
			fmt.Printf("Checking version: %d\n\n", height)

			if height > latestVersion {
				return fmt.Errorf("height %d is greater than latest version %d", height, latestVersion)
			}

			// Try to get commit info for this version
			// Note: GetCommitInfo might not be available on the interface, we'll handle it gracefully
			var commitInfo *storetypes.CommitInfo
			if rootStore, ok := cms.(interface {
				GetCommitInfo(int64) (*storetypes.CommitInfo, error)
			}); ok {
				commitInfo, err = rootStore.GetCommitInfo(height)
				if err != nil {
					fmt.Printf("⚠️  Warning: Could not get commit info for version %d: %v\n", height, err)
				} else {
					fmt.Printf("✓ Commit info found for version %d\n", height)
					fmt.Printf("  Store count in commit info: %d\n", len(commitInfo.StoreInfos))
				}
			}

			// Check each store
			fmt.Println("\nChecking IAVL stores:")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

			hasErrors := false

			// List of all store keys to check (from app/modules.go allStoreKeys())
			storeKeyNames := []string{
				authtypes.StoreKey,
				authzkeeper.StoreKey,
				banktypes.StoreKey,
				stakingtypes.StoreKey,
				minttypes.StoreKey,
				distrtypes.StoreKey,
				slashingtypes.StoreKey,
				govtypes.StoreKey,
				paramstypes.StoreKey,
				upgradetypes.StoreKey,
				feegrant.StoreKey,
				evidencetypes.StoreKey,
				ibctransfertypes.StoreKey,
				ibcexported.StoreKey,
				packetforwardtypes.StoreKey,
				icahosttypes.StoreKey,
				signaltypes.StoreKey,
				blobtypes.StoreKey,
				minfeetypes.StoreKey,
				consensustypes.StoreKey,
				circuittypes.StoreKey,
			}

			// Iterate through all store keys
			for _, storeKeyName := range storeKeyNames {
				// Get the store key
				storeKey := capp.GetKey(storeKeyName)
				if storeKey == nil {
					continue
				}

				// Get the commit KV store
				store := cms.GetCommitKVStore(storeKey)
				if store == nil {
					continue
				}

				storeType := store.GetStoreType()
				if storeType != storetypes.StoreTypeIAVL {
					continue
				}

				iavlStore, ok := store.(*iavlstore.Store)
				if !ok {
					continue
				}

				// Check if version exists
				versionExists := iavlStore.VersionExists(height)
				availableVersions := iavlStore.GetAllVersions()

				status := "✓"
				if !versionExists {
					status = "✗"
					hasErrors = true
				}

				fmt.Printf("%s Store: %s\n", status, storeKey.Name())
				fmt.Printf("   Version exists: %v\n", versionExists)
				fmt.Printf("   Current version: %d\n", iavlStore.LastCommitID().Version)

				if len(availableVersions) > 0 {
					fmt.Printf("   Available versions (sample, showing first/last 10): ")
					if len(availableVersions) <= 20 {
						fmt.Printf("%v\n", availableVersions)
					} else {
						fmt.Printf("%v ... %v\n", availableVersions[:10], availableVersions[len(availableVersions)-10:])
					}
				} else {
					fmt.Printf("   Available versions: (unable to retrieve)\n")
				}

				// Try to get immutable version
				if versionExists {
					_, err := iavlStore.GetImmutable(height)
					if err != nil {
						fmt.Printf("   ⚠️  Warning: Version exists but GetImmutable failed: %v\n", err)
						hasErrors = true
					} else {
						fmt.Printf("   ✓ GetImmutable succeeded\n")
					}
				} else if commitInfo != nil {
					// Check if store existed at this version according to commit info
					storeExisted := false
					for _, storeInfo := range commitInfo.StoreInfos {
						if storeInfo.Name == storeKey.Name() {
							storeExisted = true
							break
						}
					}
					if storeExisted {
						fmt.Printf("   ⚠️  Store existed at this version but version is missing from IAVL tree!\n")
						fmt.Printf("   This indicates the version was pruned or the IAVL tree is missing this version.\n")
					} else {
						fmt.Printf("   ℹ️  Store did not exist at this version (this is normal for new stores)\n")
					}
				}
				fmt.Println()
			}

			if hasErrors {
				fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
				fmt.Println("⚠️  Some stores have issues with this version.")
				fmt.Println("This may indicate:")
				fmt.Println("  - Versions were pruned")
				fmt.Println("  - IAVL tree is missing versions")
				fmt.Println("  - Database corruption")
				return fmt.Errorf("version check found issues")
			}

			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println("✓ All IAVL stores have the requested version available")
			return nil
		},
	}

	return cmd
}
