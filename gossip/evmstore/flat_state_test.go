package evmstore

import (
	"testing"

	"github.com/Fantom-foundation/lachesis-base/hash"
	"github.com/Fantom-foundation/lachesis-base/inter/idx"
	"github.com/Fantom-foundation/lachesis-base/kvdb/memorydb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"github.com/syndtr/goleveldb/leveldb/opt"

	"github.com/Fantom-foundation/go-opera/integration/makegenesis"
	"github.com/Fantom-foundation/go-opera/inter/iblockproc"
)

func TestFillFlatStateCache(t *testing.T) {
	require := require.New(t)

	testCase := func(i int) (addr common.Address, loc, val common.Hash) {
		key := makegenesis.FakeKey(idx.ValidatorID(i))
		addr = crypto.PubkeyToAddress(key.PublicKey)
		loc = common.BytesToHash([]byte("pub_key"))
		val = common.BytesToHash(crypto.FromECDSAPub(&key.PublicKey))
		return
	}

	const N = 10
	var (
		cfg = StoreConfig{
			EnablePreimageRecording: true,
			Cache: StoreCacheConfig{
				EvmSnap: 1 * opt.MiB,
			},
		}
		dbs  = memorydb.New()
		s    *Store
		root common.Hash
	)

	// creation
	s = NewStore(dbs, cfg)
	for i := 0; i < N; i++ {
		state, err := s.HistoryStateDB(hash.Hash(root))
		require.NoError(err)

		addr, loc, val := testCase(i)
		state.SetNonce(addr, 1)
		state.SetState(addr, loc, val)

		root, err = state.Commit(false)
		require.NoError(err)
		/*
			if i == N/2 {
				err = s.GenerateEvmSnapshot(root, false, true)
				require.NoError(err)
			}
		*/
	}

	s.Commit(iblockproc.BlockState{
		FinalizedStateRoot: hash.Hash(root),
		LastBlock: iblockproc.BlockCtx{
			Idx: 1,
		},
	}, true)

	// re-creation
	s = NewStore(dbs, cfg)

	s.FillFlatStateCache(hash.Hash(root))

	state, err := s.LastStateDB(hash.Hash(root))
	require.NoError(err)
	for i := 0; i < N; i++ {
		addr, loc, exp := testCase(i)
		val := state.GetState(addr, loc)
		require.Equal(exp, val, i)
	}
}
