package inclusion

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/da"
)

func TestGetCommitment(t *testing.T) {
	type testCase struct {
		name                 string
		dah                  da.DataAvailabilityHeader
		start                int
		blobShareLen         int
		subtreeRootThreshold int
		wantError            error
	}

	testCases := []testCase{
		{
			name:                 "should return an error if DataAvailabilityHeader is zero",
			dah:                  da.DataAvailabilityHeader{},
			start:                0,
			blobShareLen:         0,
			subtreeRootThreshold: appconsts.SubtreeRootThreshold,
			wantError:            fmt.Errorf("DataAvailabilityHeader is zero"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := GetCommitment(nil, tc.dah, tc.start, tc.blobShareLen, tc.subtreeRootThreshold)
			if tc.wantError != nil {
				assert.Error(t, err)
				assert.ErrorContains(t, err, tc.wantError.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
