// Package storage implements the storage backend.
package storage

import (
	"context"
	"crypto/rand"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	memorySigner "github.com/oasislabs/ekiden/go/common/crypto/signature/signers/memory"
	"github.com/oasislabs/ekiden/go/common/identity"
	registry "github.com/oasislabs/ekiden/go/registry/api"
	scheduler "github.com/oasislabs/ekiden/go/scheduler/api"
	"github.com/oasislabs/ekiden/go/storage/api"
	"github.com/oasislabs/ekiden/go/storage/badger"
	"github.com/oasislabs/ekiden/go/storage/cachingclient"
	"github.com/oasislabs/ekiden/go/storage/client"
	"github.com/oasislabs/ekiden/go/storage/leveldb"
)

const (
	cfgBackend             = "storage.backend"
	cfgDebugMockSigningKey = "storage.debug.mock_signing_key"
	cfgCrashEnabled        = "storage.crash.enabled"
	cfgLRUSlots            = "storage.root_cache.apply_lock_lru_slots"
	cfgInsecureSkipChecks  = "storage.debug.insecure_skip_checks"
)

// New constructs a new Backend based on the configuration flags.
func New(
	ctx context.Context,
	dataDir string,
	identity *identity.Identity,
	schedulerBackend scheduler.Backend,
	registryBackend registry.Backend,
) (api.Backend, error) {
	cfg := &api.Config{
		DB:                 dataDir,
		Signer:             identity.NodeSigner,
		ApplyLockLRUSlots:  uint64(viper.GetInt(cfgLRUSlots)),
		InsecureSkipChecks: viper.GetBool(cfgInsecureSkipChecks),
	}

	var err error
	if viper.GetBool(cfgDebugMockSigningKey) {
		cfg.Signer, err = memorySigner.NewSigner(rand.Reader)
		if err != nil {
			return nil, err
		}
	}

	var impl api.Backend
	backend := viper.GetString(cfgBackend)
	switch strings.ToLower(backend) {
	case badger.BackendName:
		cfg.DB = filepath.Join(cfg.DB, badger.DBFile)
		impl, err = badger.New(cfg)
	case leveldb.BackendName:
		cfg.DB = filepath.Join(cfg.DB, leveldb.DBFile)
		impl, err = leveldb.New(cfg)
	case client.BackendName:
		impl, err = client.New(ctx, identity, schedulerBackend, registryBackend)
	case cachingclient.BackendName:
		var remote api.Backend
		remote, err = client.New(ctx, identity, schedulerBackend, registryBackend)
		if err != nil {
			return nil, err
		}
		impl, err = cachingclient.New(remote, cfg.InsecureSkipChecks)
	default:
		err = fmt.Errorf("storage: unsupported backend: '%v'", backend)
	}

	crashEnabled := viper.GetBool(cfgCrashEnabled)
	if crashEnabled {
		impl = newCrashingWrapper(impl)
	}

	if err != nil {
		return nil, err
	}

	return newMetricsWrapper(impl), nil
}

// RegisterFlags registers the configuration flags with the provided
// command.
func RegisterFlags(cmd *cobra.Command) {
	if !cmd.Flags().Parsed() {
		cmd.Flags().String(cfgBackend, leveldb.BackendName, "Storage backend")
		cmd.Flags().Bool(cfgDebugMockSigningKey, false, "Generate volatile mock signing key")
		cmd.Flags().Bool(cfgCrashEnabled, false, "Enable the crashing storage wrapper")
		cmd.Flags().Int(cfgLRUSlots, 1000, "How many LRU slots to use for Apply call locks in the MKVS tree root cache")

		cmd.Flags().Bool(cfgInsecureSkipChecks, false, "INSECURE: Skip known root checks")
		_ = cmd.Flags().MarkHidden(cfgInsecureSkipChecks)
	}

	for _, v := range []string{
		cfgBackend,
		cfgDebugMockSigningKey,
		cfgCrashEnabled,
		cfgLRUSlots,
		cfgInsecureSkipChecks,
	} {
		viper.BindPFlag(v, cmd.Flags().Lookup(v)) //nolint: errcheck
	}

	client.RegisterFlags(cmd)
	cachingclient.RegisterFlags(cmd)
}
