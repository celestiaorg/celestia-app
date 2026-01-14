package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/x/blob/keeper"
	blobtypes "github.com/celestiaorg/celestia-app/v7/x/blob/types"
	"github.com/stretchr/testify/require"
)

func TestMigrateParams(t *testing.T) {
	tests := []struct {
		name           string
		expectedParams blobtypes.Params
	}{
		{
			name:           "success",
			expectedParams: blobtypes.DefaultParams(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, _, ctx := CreateKeeper(t, appconsts.Version)
			migrator := keeper.NewMigrator(*k)
			err := migrator.MigrateParams(ctx)
			require.NoError(t, err)
			require.Equal(t, tt.expectedParams, k.GetParams(ctx))
		})
	}
}
