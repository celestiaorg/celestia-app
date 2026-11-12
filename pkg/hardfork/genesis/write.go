package genesis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"math"
	"math/bits"
	"runtime"

	abci "github.com/cometbft/cometbft/abci/types"
	cryptoenc "github.com/cometbft/cometbft/crypto/encoding"
)

const (
	// embeddedServerLimit is the gRPC receive limit of an embedded older app (celestia-core abci/server/grpc_server.go).
	embeddedServerLimit = 75 << 20
	candidateStatus     = "candidate: do not publish until trial-import passes for genesis_sha256"
)

// Sizes are the byte counts of a written genesis.
type Sizes struct {
	File     int64 `json:"file"`
	AppState int64 `json:"app_state"`
	// InitChain is the proto size of the RequestInitChain that core builds from the file.
	InitChain int64 `json:"init_chain_request"`
}

// Written is the SHA-256 and sizes of a written genesis.
type Written struct {
	SHA256 [sha256.Size]byte
	Sizes  Sizes
}

// Change is an outer field in the export and in the candidate.
type Change struct {
	Export    string `json:"export"`
	Candidate string `json:"candidate"`
}

// Report records a candidate genesis and what changed from the export.
type Report struct {
	Status          string `json:"status"`
	ToolGoVersion   string `json:"tool_go_version"`
	ExportSHA256    string `json:"export_sha256"`
	ExportHeight    int64  `json:"export_height"`
	ExportBlockTime string `json:"export_block_time"`
	AppName         string `json:"app_name"`
	AppVersion      string `json:"app_version"`
	ChainID         Change `json:"chain_id"`
	InitialHeight   Change `json:"initial_height"`
	GenesisTime     Change `json:"genesis_time"`
	GenesisSHA256   string `json:"genesis_sha256"`
	Sizes           Sizes  `json:"sizes"`
	Transport       string `json:"transport,omitempty"`
}

// NewReport returns the report of cand built from exp; Install fills the genesis hash and sizes.
func NewReport(exp *Export, cand *Genesis, opts Options) *Report {
	return &Report{
		Status:          candidateStatus,
		ToolGoVersion:   runtime.Version(),
		ExportSHA256:    hex.EncodeToString(exp.SHA256[:]),
		ExportHeight:    exp.InitialHeight - 1,
		ExportBlockTime: formatTime(opts.ExportBlockTime),
		AppName:         exp.AppName,
		AppVersion:      exp.AppVersion,
		ChainID:         Change{exp.ChainID, cand.ChainID},
		InitialHeight:   Change{fmt.Sprint(exp.InitialHeight), fmt.Sprint(cand.InitialHeight)},
		GenesisTime:     Change{formatTime(exp.GenesisTime), formatTime(cand.GenesisTime)},
	}
}

// countWriter hashes and counts the bytes written to w.
type countWriter struct {
	w io.Writer
	h hash.Hash
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.h.Write(p[:n])
	c.n += int64(n)
	return n, err
}

// WriteGenesis writes g with the bytes of json.Marshal of the same AppGenesis, one field and one module at a time.
// The genesis time is written in UTC.
func WriteGenesis(w io.Writer, g *Genesis) (Written, error) {
	cw := &countWriter{w: w, h: sha256.New()}
	var err error
	put := func(s string) {
		if err == nil {
			_, err = io.WriteString(cw, s)
		}
	}
	val := func(v any) {
		if err == nil {
			var b []byte
			// json.Marshal compacts and HTML-escapes raw values as it does inside AppGenesis.
			if b, err = json.Marshal(v); err == nil {
				_, err = cw.Write(b)
			}
		}
	}

	put(`{"app_name":`)
	val(g.AppName)
	put(`,"app_version":`)
	val(g.AppVersion)
	put(`,"genesis_time":`)
	val(g.GenesisTime.UTC())
	put(`,"chain_id":`)
	val(g.ChainID)
	put(`,"initial_height":`)
	val(g.InitialHeight)
	put(`,"app_hash":`)
	val(g.AppHash)
	put(`,"app_state":`)
	start := cw.n
	put("{")
	for i, m := range g.Modules {
		if i > 0 {
			put(",")
		}
		val(m.Name)
		put(":")
		val(m.Raw)
	}
	put("}")
	appState := cw.n - start
	if g.Consensus != nil {
		put(`,"consensus":`)
		val(g.Consensus)
	}
	put("}")
	if err != nil {
		return Written{}, err
	}

	initChain, err := initChainSize(g, appState)
	if err != nil {
		return Written{}, err
	}
	out := Written{Sizes: Sizes{File: cw.n, AppState: appState, InitChain: initChain}}
	cw.h.Sum(out.SHA256[:0])
	return out, nil
}

// initChainSize is the proto size of core's RequestInitChain with appState bytes of app_state, without copying them.
func initChainSize(g *Genesis, appState int64) (int64, error) {
	req := abci.RequestInitChain{Time: g.GenesisTime, ChainId: g.ChainID, InitialHeight: g.InitialHeight}
	if g.Consensus != nil {
		if g.Consensus.Params != nil {
			params := g.Consensus.Params.ToProto()
			req.ConsensusParams = &params
		}
		for i, v := range g.Consensus.Validators {
			pk, err := cryptoenc.PubKeyToProto(v.PubKey)
			if err != nil {
				return 0, fmt.Errorf("consensus.validators[%d].pub_key: %w", i, err)
			}
			req.Validators = append(req.Validators, abci.ValidatorUpdate{PubKey: pk, Power: v.Power})
		}
	}
	// Tag, length varint and bytes of app_state_bytes (field 5).
	return int64(req.Size()) + 1 + int64(bits.Len64(uint64(appState)|1)+6)/7 + appState, nil
}

// transportNote warns when the InitChain request exceeds a gRPC limit.
func transportNote(initChain int64) string {
	switch {
	case initChain > math.MaxInt32:
		return "InitChain request above MaxInt32 bytes: only a native start (standalone, or the multiplexer's native version) can import it"
	case initChain > embeddedServerLimit:
		return "InitChain request above 75 MiB: an embedded older app version cannot import it; a native start can"
	}
	return ""
}
