package testutil

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"

	"github.com/gogo/protobuf/proto"

	minttypes "github.com/celestiaorg/celestia-app/x/mint/types"
)

func (s *IntegrationTestSuite) TestQueryGRPC() {
	val := s.network.Validators[0]
	baseURL := val.APIAddress
	testCases := []struct {
		name     string
		url      string
		headers  map[string]string
		respType proto.Message
		expected proto.Message
	}{
		{
			"gRPC request inflation rate",
			fmt.Sprintf("%s/cosmos/mint/v1beta1/inflation_rate", baseURL),
			map[string]string{},
			&minttypes.QueryInflationRateResponse{},
			&minttypes.QueryInflationRateResponse{
				InflationRate: sdk.NewDecWithPrec(8, 2),
			},
		},
		{
			"gRPC request annual provisions",
			fmt.Sprintf("%s/cosmos/mint/v1beta1/annual_provisions", baseURL),
			map[string]string{
				grpctypes.GRPCBlockHeightHeader: "1",
			},
			&minttypes.QueryAnnualProvisionsResponse{},
			&minttypes.QueryAnnualProvisionsResponse{
				AnnualProvisions: sdk.NewDec(40_000_000),
			},
		},
	}
	for _, tc := range testCases {
		resp, err := testutil.GetRequestWithHeaders(tc.url, tc.headers)
		s.Run(tc.name, func() {
			s.Require().NoError(err)
			s.Require().NoError(val.ClientCtx.Codec.UnmarshalJSON(resp, tc.respType))
			s.Require().Equal(tc.expected.String(), tc.respType.String())
		})
	}
}
