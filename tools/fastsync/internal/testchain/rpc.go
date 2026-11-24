package testchain

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	cmtjson "github.com/cometbft/cometbft/libs/json"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
)

// Forge may alter a result (a *coretypes.ResultCommit, *ResultValidators or
// *ResultConsensusParams) before the server returns it.
type Forge func(method string, result any)

// ServeRPC serves the JSON-RPC calls a light client and a state provider make
// (commit, validators, consensus_params) for the chain.
func (c *Chain) ServeRPC(t testing.TB, forge Forge) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage            `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		res, err := c.rpcResult(req.Method, req.Params)
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if err == nil && forge != nil {
			forge(req.Method, res)
		}
		if err == nil {
			var bz []byte
			if bz, err = cmtjson.Marshal(res); err == nil {
				resp["result"] = json.RawMessage(bz)
			}
		}
		if err != nil {
			resp["error"] = map[string]any{"code": -32603, "message": "Internal error", "data": err.Error()}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func intParam(params map[string]json.RawMessage, key string, def int64) (int64, error) {
	raw, ok := params[key]
	if !ok || string(raw) == "null" {
		return def, nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strconv.ParseInt(s, 10, 64)
	}
	var n int64
	err := json.Unmarshal(raw, &n)
	return n, err
}

func (c *Chain) rpcResult(method string, params map[string]json.RawMessage) (any, error) {
	h, err := intParam(params, "height", c.N())
	if err != nil {
		return nil, err
	}
	if h < 1 || h > c.N() {
		return nil, fmt.Errorf("height %d is not available (chain at %d)", h, c.N())
	}
	switch method {
	case "commit":
		hd := *c.Header(h)
		commit := *c.Commits[h]
		return coretypes.NewResultCommit(&hd, &commit, h < c.N()), nil
	case "validators":
		page, err := intParam(params, "page", 1)
		if err != nil {
			return nil, err
		}
		perPage, err := intParam(params, "per_page", 30)
		if err != nil {
			return nil, err
		}
		vs := c.Vals[h].Copy()
		lo := min((page-1)*perPage, int64(len(vs.Validators)))
		hi := min(lo+perPage, int64(len(vs.Validators)))
		return &coretypes.ResultValidators{BlockHeight: h, Validators: vs.Validators[lo:hi],
			Count: int(hi - lo), Total: len(vs.Validators)}, nil
	case "consensus_params":
		return &coretypes.ResultConsensusParams{BlockHeight: h, ConsensusParams: c.Params}, nil
	}
	return nil, fmt.Errorf("method %q not served", method)
}
