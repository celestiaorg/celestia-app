package keeper

// TODO uncomment after adding logic for other messages
// func TestQueryValsetConfirm(t *testing.T) {
//	var (
//		addrStr                       = "qgb1ees2tqhhhm9ahlhceh2zdguww9lqn2ckcxpllh"
//		nonce                         = uint64(1)
//		myValidatorCosmosAddr, err1   = sdk.AccAddressFromBech32(addrStr)
//		myValidatorEthereumAddr, err2 = types.NewEthAddress("0x3232323232323232323232323232323232323232")
//	)
//	require.NoError(t, err1)
//	require.NoError(t, err2)
//	input := CreateTestEnv(t)
//	sdkCtx := input.Context
//	ctx := sdk.WrapSDKContext(input.Context)
//	k := input.QgbKeeper
//	input.QgbKeeper.SetValsetConfirm(sdkCtx, types.MsgValsetConfirm{
//		Nonce:        nonce,
//		Orchestrator: myValidatorCosmosAddr.String(),
//		EthAddress:   myValidatorEthereumAddr.GetAddress(),
//		Signature:    "alksdjhflkasjdfoiasjdfiasjdfoiasdj",
//	})
//
//	specs := map[string]struct {
//		src     types.QueryValsetConfirmRequest
//		expErr  bool
//		expResp types.QueryValsetConfirmResponse
//	}{
//		/*  Nonce        uint64 `protobuf:"varint,1,opt,name=nonce,proto3" json:"nonce,omitempty"`
//		    Orchestrator string `protobuf:"bytes,2,opt,name=orchestrator,proto3" json:"orchestrator,omitempty"`
//		    EthAddress   string `protobuf:"bytes,3,opt,name=eth_address,json=ethAddress,proto3" json:"eth_address,omitempty"`
//		    Signature    string `protobuf:"bytes,4,opt,name=signature,proto3" json:"signature,omitempty"`
//		}*/
//
//		"all good": {
//			src: types.QueryValsetConfirmRequest{Nonce: 1, Address: myValidatorCosmosAddr.String()},
//
//			//expResp:  []byte(`{"type":"gravity/MsgValsetConfirm", "value":{"eth_address":"0x3232323232323232323232323232323232323232", "nonce": "1", "orchestrator": "cosmos1ees2tqhhhm9ahlhceh2zdguww9lqn2ckukn86l",  "signature": "alksdjhflkasjdfoiasjdfiasjdfoiasdj"}}`),
//			expResp: types.QueryValsetConfirmResponse{
//				Confirm: types.NewMsgValsetConfirm(1, *myValidatorEthereumAddr, myValidatorCosmosAddr, "alksdjhflkasjdfoiasjdfiasjdfoiasdj")},
//			expErr: false,
//		},
//		"unknown nonce": {
//			src:     types.QueryValsetConfirmRequest{Nonce: 999999, Address: myValidatorCosmosAddr.String()},
//			expResp: types.QueryValsetConfirmResponse{Confirm: nil},
//		},
//		"invalid address": {
//			src:    types.QueryValsetConfirmRequest{1, "not a valid addr"},
//			expErr: true,
//		},
//	}
//
//	for msg, spec := range specs {
//		t.Run(msg, func(t *testing.T) {
//			got, err := k.ValsetConfirm(ctx, &spec.src)
//			if spec.expErr {
//				require.Error(t, err)
//				return
//			}
//			require.NoError(t, err)
//			if spec.expResp == (types.QueryValsetConfirmResponse{}) {
//				assert.True(t, got == nil || got.Confirm == nil)
//				return
//			}
//			assert.Equal(t, &spec.expResp, got)
//		})
//	}
//}
//
////nolint: exhaustivestruct
//func TestAllValsetConfirmsBynonce(t *testing.T) {
//	addrs := []string{
//		"gravity1u508cfnsk2nhakv80vdtq3nf558ngyvlfxm2hd",
//		"gravity1krtcsrxhadj54px0vy6j33pjuzcd3jj8jtz98y",
//		"gravity1u94xef3cp9thkcpxecuvhtpwnmg8mhljeh96n9",
//	}
//	var (
//		nonce                       = uint64(1)
//		myValidatorCosmosAddr1, _   = sdk.AccAddressFromBech32(addrs[0])
//		myValidatorCosmosAddr2, _   = sdk.AccAddressFromBech32(addrs[1])
//		myValidatorCosmosAddr3, _   = sdk.AccAddressFromBech32(addrs[2])
//		myValidatorEthereumAddr1, _ = types.NewEthAddress("0x0101010101010101010101010101010101010101")
//		myValidatorEthereumAddr2, _ = types.NewEthAddress("0x0202020202020202020202020202020202020202")
//		myValidatorEthereumAddr3, _ = types.NewEthAddress("0x0303030303030303030303030303030303030303")
//	)
//
//	input := CreateTestEnv(t)
//	sdkCtx := input.Context
//	ctx := sdk.WrapSDKContext(input.Context)
//	k := input.QgbKeeper
//
//	// seed confirmations
//	for i := 0; i < 3; i++ {
//		addr, _ := sdk.AccAddressFromBech32(addrs[i])
//		msg := types.MsgValsetConfirm{}
//		msg.EthAddress = gethcommon.BytesToAddress(bytes.Repeat([]byte{byte(i + 1)}, 20)).String()
//		msg.Nonce = uint64(1)
//		msg.Orchestrator = addr.String()
//		msg.Signature = fmt.Sprintf("signature %d", i+1)
//		input.QgbKeeper.SetValsetConfirm(sdkCtx, msg)
//	}
//
//	specs := map[string]struct {
//		src     types.QueryValsetConfirmsByNonceRequest
//		expErr  bool
//		expResp types.QueryValsetConfirmsByNonceResponse
//	}{
//		"all good": {
//			src: types.QueryValsetConfirmsByNonceRequest{Nonce: 1},
//			expResp: types.QueryValsetConfirmsByNonceResponse{Confirms: []types.MsgValsetConfirm{
//				*types.NewMsgValsetConfirm(nonce, *myValidatorEthereumAddr2, myValidatorCosmosAddr2, "signature 2"),
//				*types.NewMsgValsetConfirm(nonce, *myValidatorEthereumAddr3, myValidatorCosmosAddr3, "signature 3"),
//				*types.NewMsgValsetConfirm(nonce, *myValidatorEthereumAddr1, myValidatorCosmosAddr1, "signature 1"),
//			}},
//		},
//		"unknown nonce": {
//			src:     types.QueryValsetConfirmsByNonceRequest{Nonce: 999999},
//			expResp: types.QueryValsetConfirmsByNonceResponse{},
//		},
//	}
//	for msg, spec := range specs {
//		t.Run(msg, func(t *testing.T) {
//			got, err := k.ValsetConfirmsByNonce(ctx, &types.QueryValsetConfirmsByNonceRequest{Nonce: spec.src.Nonce})
//			if spec.expErr {
//				require.Error(t, err)
//				return
//			}
//			require.NoError(t, err)
//			assert.Equal(t, &(spec.expResp), got)
//		})
//	}
//}
