package testutil

import (
	"fmt"
	"strings"

	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"

	"github.com/gogo/protobuf/proto"

	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	mint "github.com/celestiaorg/celestia-app/v2/x/mint/types"
)

func (s *IntegrationTestSuite) TestQueryGRPC() {
	baseURL := s.cctx.APIAddress()
	baseURL = strings.Replace(baseURL, "tcp", "http", 1)
	expectedAnnualProvision := mint.InitialInflationRateAsDec().MulInt(sdk.NewInt(testnode.DefaultInitialBalance))
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
			&mint.QueryInflationRateResponse{},
			&mint.QueryInflationRateResponse{
				InflationRate: sdk.NewDecWithPrec(8, 2),
			},
		},
		{
			"gRPC request annual provisions",
			fmt.Sprintf("%s/cosmos/mint/v1beta1/annual_provisions", baseURL),
			map[string]string{
				grpctypes.GRPCBlockHeightHeader: "1",
			},
			&mint.QueryAnnualProvisionsResponse{},
			&mint.QueryAnnualProvisionsResponse{
				AnnualProvisions: expectedAnnualProvision,
			},
		},
	}
	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := testutil.GetRequestWithHeaders(tc.url, tc.headers)
			s.Require().NoError(err)
			s.Require().NoError(s.cctx.Context.Codec.UnmarshalJSON(resp, tc.respType))
			s.Require().Equal(tc.expected.String(), tc.respType.String())
		})
	}
}
