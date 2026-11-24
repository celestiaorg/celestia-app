package hdrchain

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

// Fixtures: /commit and /validators responses from a mainnet archive node
// (celestia-core 0.38.17): genesis, and the v3 -> v4 boundary (6,680,340 is
// the first header produced under the v0.38-based celestia-core ABCI 2 path).
const mainnetChainID = "celestia"

var mainnetRuns = [][]int64{
	{1, 2},
	{6680339, 6680340, 6680341},
}

type fixtureSource struct {
	t   testing.TB
	dir string
}

func (f fixtureSource) read(name string) []byte {
	bz, err := os.ReadFile(filepath.Join(f.dir, name))
	require.NoError(f.t, err)
	return bz
}

func (f fixtureSource) SignedHeader(_ context.Context, h int64) (*types.SignedHeader, error) {
	sh, _, err := DecodeCommitResponse(f.read(fmt.Sprintf("commit_%d.json", h)))
	return sh, err
}

func (f fixtureSource) Validators(_ context.Context, h int64) (*types.ValidatorSet, error) {
	p, err := DecodeValidatorsPage(f.read(fmt.Sprintf("validators_%d.json", h)))
	if err != nil {
		return nil, err
	}
	return ValidatorSetFromPages(h, p)
}

func mainnet(t testing.TB) fixtureSource {
	return fixtureSource{t: t, dir: filepath.Join("testdata", "mainnet")}
}

func load(t testing.TB, h int64) (*types.SignedHeader, *types.ValidatorSet) {
	src := mainnet(t)
	sh, err := src.SignedHeader(context.Background(), h)
	require.NoError(t, err)
	vals, err := src.Validators(context.Background(), h)
	require.NoError(t, err)
	return sh, vals
}

func TestMainnetCommitsVerify(t *testing.T) {
	for _, run := range mainnetRuns {
		for _, h := range run {
			sh, vals := load(t, h)
			ht, err := ForAppVersion(sh.Version.Block, sh.Version.App)
			require.NoError(t, err)
			require.NoError(t, CheckCommit(ht, mainnetChainID, sh, vals, false), "height %d light", h)
			require.NoError(t, CheckCommit(ht, mainnetChainID, sh, vals, true), "height %d all signatures", h)
		}
	}
}

func TestMainnetLinksAcrossUpgrades(t *testing.T) {
	ht := CometBFT{}
	for _, run := range mainnetRuns {
		for i := 1; i < len(run); i++ {
			prev, _ := load(t, run[i-1])
			next, _ := load(t, run[i])
			require.NoError(t, CheckLink(ht, prev.Header, next.Header), "%d -> %d", run[i-1], run[i])
			require.NoError(t, CheckCommitBinding(ht, prev.Commit, next.Header), "commit %d", run[i-1])
		}
	}
	// The v0.34 -> v0.38 boundary: header 6680340 (app 4) links to 6680339 (app 3).
	prev, _ := load(t, 6680339)
	next, _ := load(t, 6680340)
	require.EqualValues(t, 3, prev.Version.App)
	require.EqualValues(t, 4, next.Version.App)
	require.Equal(t, []byte(prev.Hash()), []byte(next.LastBlockID.Hash))
}

func TestMainnetForwardFromGenesis(t *testing.T) {
	src := mainnet(t)
	_, vals1 := load(t, 1)
	v, err := NewGenesisVerifier(mainnetChainID, 1, vals1.Hash())
	require.NoError(t, err)
	st, err := RunForward(context.Background(), src, v, 1, 2, nil)
	require.NoError(t, err)
	require.EqualValues(t, 2, st.Headers)
	require.EqualValues(t, 1, st.ValidatorSets, "set unchanged after genesis: fetched once")
	require.EqualValues(t, 2, v.Last().Height)

	// Wrong genesis root.
	bad := bytes.Repeat([]byte{1}, 32)
	v, err = NewGenesisVerifier(mainnetChainID, 1, bad)
	require.NoError(t, err)
	_, err = RunForward(context.Background(), src, v, 1, 1, nil)
	require.ErrorContains(t, err, "genesis validator set hash")
}

func TestMainnetForwardAcrossV4(t *testing.T) {
	src := mainnet(t)
	trusted, vals := load(t, 6680339)
	v, err := NewTrustedVerifier(mainnetChainID, trusted, trusted.Hash(), vals)
	require.NoError(t, err)
	var changes []string
	v.OnAppVersion = func(h int64, from, to uint64) { changes = append(changes, fmt.Sprintf("%d:%d->%d", h, from, to)) }
	st, err := RunForward(context.Background(), src, v, 6680340, 6680341, nil)
	require.NoError(t, err)
	require.EqualValues(t, 2, st.Headers)
	require.Equal(t, []string{"6680340:3->4"}, changes)
	require.LessOrEqual(t, st.ValidatorSets, int64(2))
}

func TestMainnetBackwardHashChain(t *testing.T) {
	run := mainnetRuns[1]
	top, _ := load(t, run[len(run)-1])
	var hs []*types.Header
	for i := len(run) - 1; i >= 0; i-- {
		sh, _ := load(t, run[i])
		hs = append(hs, sh.Header)
	}
	below, err := VerifyBackward(mainnetChainID, top.Hash(), hs)
	require.NoError(t, err)
	first, _ := load(t, run[0])
	require.Equal(t, []byte(first.LastBlockID.Hash), below)

	// Tamper with an interior header's app hash.
	hs[1].AppHash = bytes.Repeat([]byte{0xAA}, 32)
	_, err = VerifyBackward(mainnetChainID, top.Hash(), hs)
	require.ErrorContains(t, err, "hash chain broken at height 6680340")
}

func TestMainnetTampering(t *testing.T) {
	ht := CometBFT{}
	cases := map[string]func(sh *types.SignedHeader, vals **types.ValidatorSet){
		"header field": func(sh *types.SignedHeader, _ **types.ValidatorSet) {
			sh.AppHash = bytes.Repeat([]byte{1}, 32)
		},
		"signature byte": func(sh *types.SignedHeader, _ **types.ValidatorSet) {
			for i := range sh.Commit.Signatures {
				if sh.Commit.Signatures[i].BlockIDFlag == types.BlockIDFlagCommit {
					sh.Commit.Signatures[i].Signature[0] ^= 1
					return
				}
			}
		},
		"validator dropped": func(_ *types.SignedHeader, vals **types.ValidatorSet) {
			vs := (*vals).Copy()
			vs.Validators = vs.Validators[:len(vs.Validators)-1]
			*vals = vs
		},
		"voting power changed": func(_ *types.SignedHeader, vals **types.ValidatorSet) {
			vs := (*vals).Copy()
			vs.Validators[0].VotingPower++
			*vals = vs
		},
		"under 2/3 signed": func(sh *types.SignedHeader, vals **types.ValidatorSet) {
			// Mark signatures absent until only 1/2 of power remains.
			var signed int64
			for i, cs := range sh.Commit.Signatures {
				if cs.BlockIDFlag == types.BlockIDFlagCommit {
					signed += (*vals).Validators[i].VotingPower
				}
			}
			for i := range sh.Commit.Signatures {
				if signed*2 <= (*vals).TotalVotingPower() {
					break
				}
				if sh.Commit.Signatures[i].BlockIDFlag == types.BlockIDFlagCommit {
					signed -= (*vals).Validators[i].VotingPower
					sh.Commit.Signatures[i] = types.NewCommitSigAbsent()
				}
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			sh, vals := load(t, 6680340)
			mutate(sh, &vals)
			require.Error(t, CheckCommit(ht, mainnetChainID, sh, vals, true))
		})
	}
	t.Run("wrong chain id", func(t *testing.T) {
		sh, vals := load(t, 6680340)
		require.ErrorContains(t, CheckCommit(ht, "mocha-4", sh, vals, false), "another chain")
	})
	t.Run("non canonical previous commit", func(t *testing.T) {
		prev, _ := load(t, 6680339)
		next, _ := load(t, 6680340)
		prev.Commit.Signatures[0] = types.NewCommitSigAbsent()
		require.ErrorContains(t, CheckCommitBinding(ht, prev.Commit, next.Header), "LastCommitHash")
	})
	t.Run("skipped height", func(t *testing.T) {
		prev, _ := load(t, 6680339)
		next, _ := load(t, 6680341)
		require.ErrorContains(t, CheckLink(ht, prev.Header, next.Header), "does not follow")
	})
}

func TestDecodeRejectsBadInput(t *testing.T) {
	for _, raw := range []string{
		``, `{}`, `not json`, `{"jsonrpc":"2.0","id":-1,"error":{"code":-32603,"message":"Internal error","data":"height 1 is not available"}}`,
		`{"result":{"signed_header":{}}}`, `{"result":{"signed_header":{"header":{"height":"x"}}}}`,
	} {
		_, _, err := DecodeCommitResponse([]byte(raw))
		require.Error(t, err, raw)
	}
	for _, raw := range []string{
		`{"result":{"block_height":"1","validators":[],"count":"1","total":"1"}}`,
		`{"result":{"block_height":"1","validators":[],"count":"0","total":"100000"}}`,
		`{"result":{"block_height":"1","validators":[],"count":"0","total":"-1"}}`,
	} {
		_, err := DecodeValidatorsPage([]byte(raw))
		require.Error(t, err, raw)
	}
}

func FuzzDecodeCommitResponse(f *testing.F) {
	f.Add(mainnet(f).read("commit_6680340.json"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		sh, _, err := DecodeCommitResponse(raw)
		if err != nil {
			return
		}
		_ = sh.ValidateBasic(mainnetChainID)
	})
}
