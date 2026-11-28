package hdrchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	cmtjson "github.com/cometbft/cometbft/libs/json"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cometbft/cometbft/types"
)

const (
	// maxResponseBytes bounds one RPC response body.
	maxResponseBytes = 32 << 20
	// maxValidators bounds a validator set read from an untrusted source.
	// CometBFT caps the set at MaxVotesCount (10000).
	maxValidators = types.MaxVotesCount
	perPage       = 100
)

type rpcEnvelope struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    string `json:"data"`
	} `json:"error"`
}

func unwrap(raw []byte) ([]byte, error) {
	var env rpcEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode JSON-RPC envelope: %w", err)
	}
	if env.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s %s", env.Error.Code, env.Error.Message, env.Error.Data)
	}
	if len(env.Result) == 0 {
		return nil, errors.New("empty JSON-RPC result")
	}
	return env.Result, nil
}

// DecodeCommitResponse decodes a /commit JSON-RPC response (header + commit).
// Values are not validated here; the Verifier does that.
func DecodeCommitResponse(raw []byte) (*types.SignedHeader, bool, error) {
	res, err := unwrap(raw)
	if err != nil {
		return nil, false, err
	}
	var rc coretypes.ResultCommit
	if err := cmtjson.Unmarshal(res, &rc); err != nil {
		return nil, false, fmt.Errorf("decode /commit result: %w", err)
	}
	if rc.Header == nil || rc.Commit == nil {
		return nil, false, errors.New("/commit result without header or commit")
	}
	sh := rc.SignedHeader
	return &sh, rc.CanonicalCommit, nil
}

// DecodeHeaderResponse decodes a /header JSON-RPC response.
func DecodeHeaderResponse(raw []byte) (*types.Header, error) {
	res, err := unwrap(raw)
	if err != nil {
		return nil, err
	}
	var rh coretypes.ResultHeader
	if err := cmtjson.Unmarshal(res, &rh); err != nil {
		return nil, fmt.Errorf("decode /header result: %w", err)
	}
	if rh.Header == nil {
		return nil, errors.New("/header result without header")
	}
	return rh.Header, nil
}

// DecodeValidatorsPage decodes one /validators JSON-RPC response page.
func DecodeValidatorsPage(raw []byte) (*coretypes.ResultValidators, error) {
	res, err := unwrap(raw)
	if err != nil {
		return nil, err
	}
	var rv coretypes.ResultValidators
	if err := cmtjson.Unmarshal(res, &rv); err != nil {
		return nil, fmt.Errorf("decode /validators result: %w", err)
	}
	if rv.Total < 0 || rv.Total > maxValidators || rv.Count != len(rv.Validators) || len(rv.Validators) > rv.Total {
		return nil, fmt.Errorf("bad /validators page: total %d count %d len %d", rv.Total, rv.Count, len(rv.Validators))
	}
	return &rv, nil
}

// ValidatorSetFromPages joins /validators pages for one height into a set in
// canonical order. It checks the pages agree on height and total.
func ValidatorSetFromPages(height int64, pages ...*coretypes.ResultValidators) (*types.ValidatorSet, error) {
	if len(pages) == 0 {
		return nil, errors.New("no validator pages")
	}
	total := pages[0].Total
	var all []*types.Validator
	for i, p := range pages {
		if p.BlockHeight != height || p.Total != total {
			return nil, fmt.Errorf("validator page %d: height %d total %d, want %d/%d", i+1, p.BlockHeight, p.Total, height, total)
		}
		all = append(all, p.Validators...)
		if len(all) > total {
			return nil, fmt.Errorf("validator pages hold more than total %d", total)
		}
	}
	if len(all) != total {
		return nil, fmt.Errorf("validator pages hold %d of %d validators", len(all), total)
	}
	return types.ValidatorSetFromExistingValidators(all)
}

// HTTPSource reads from CometBFT JSON-RPC endpoints (archive nodes). Any
// endpoint will do: everything is verified. Requests rotate through the
// endpoints on failure.
type HTTPSource struct {
	Endpoints []string
	Client    *http.Client
	Retries   int
}

// NewHTTPSource returns a source over the given base URLs.
func NewHTTPSource(endpoints ...string) *HTTPSource {
	return &HTTPSource{Endpoints: endpoints, Client: &http.Client{Timeout: 60 * time.Second}, Retries: 4}
}

func (s *HTTPSource) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	if len(s.Endpoints) == 0 {
		return nil, errors.New("no RPC endpoints")
	}
	var lastErr error
	for attempt := 0; attempt <= s.Retries; attempt++ {
		base := strings.TrimRight(s.Endpoints[attempt%len(s.Endpoints)], "/")
		body, err := s.getOnce(ctx, base+"/"+path+"?"+q.Encode())
		if err == nil {
			return body, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 250 * time.Millisecond):
		}
	}
	return nil, lastErr
}

func (s *HTTPSource) getOnce(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("%s: response larger than %d bytes", u, maxResponseBytes)
	}
	if resp.StatusCode != http.StatusOK && len(body) == 0 {
		return nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return body, nil
}

// SignedHeader implements Source with /commit.
func (s *HTTPSource) SignedHeader(ctx context.Context, h int64) (*types.SignedHeader, error) {
	raw, err := s.get(ctx, "commit", url.Values{"height": {strconv.FormatInt(h, 10)}})
	if err != nil {
		return nil, err
	}
	sh, _, err := DecodeCommitResponse(raw)
	return sh, err
}

// Validators implements Source with paginated /validators.
func (s *HTTPSource) Validators(ctx context.Context, h int64) (*types.ValidatorSet, error) {
	var pages []*coretypes.ResultValidators
	for page := 1; ; page++ {
		raw, err := s.get(ctx, "validators", url.Values{
			"height": {strconv.FormatInt(h, 10)}, "page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)},
		})
		if err != nil {
			return nil, err
		}
		p, err := DecodeValidatorsPage(raw)
		if err != nil {
			return nil, err
		}
		pages = append(pages, p)
		if page*perPage >= p.Total || len(p.Validators) == 0 {
			break
		}
	}
	return ValidatorSetFromPages(h, pages...)
}
