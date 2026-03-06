package abci

import (
	"sync"

	cmtdb "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
)

// CaptureDBProvider wraps cmtcfg.DefaultDBProvider to capture CometBFT DB handles
// for use by the background migration.
type CaptureDBProvider struct {
	mu       sync.Mutex
	captured map[string]cmtdb.DB
}

// NewCaptureDBProvider creates a new CaptureDBProvider.
func NewCaptureDBProvider() *CaptureDBProvider {
	return &CaptureDBProvider{
		captured: make(map[string]cmtdb.DB),
	}
}

// Provide satisfies the cmtcfg.DBProvider signature. It delegates to DefaultDBProvider
// and captures the returned DB handle.
func (c *CaptureDBProvider) Provide(ctx *cmtcfg.DBContext) (cmtdb.DB, error) {
	db, err := cmtcfg.DefaultDBProvider(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.captured[ctx.ID] = db
	c.mu.Unlock()
	return db, nil
}

// GetAll returns all captured DB handles.
func (c *CaptureDBProvider) GetAll() map[string]cmtdb.DB {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string]cmtdb.DB, len(c.captured))
	for k, v := range c.captured {
		result[k] = v
	}
	return result
}
