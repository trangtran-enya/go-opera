package evmstore

import (
	"time"

	"github.com/Fantom-foundation/lachesis-base/hash"
	"github.com/Fantom-foundation/lachesis-base/kvdb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

// LastStateDB returns last state database with flat state cache.
// Note: it properly works only for the last state root.
func (s *Store) LastStateDB(from hash.Hash) (*FlatStateDB, error) {
	underliyng, err := s.stateDB(from)
	if err != nil {
		return nil, err
	}

	return &FlatStateDB{
		StateDB: underliyng,
		flat:    s.table.FlatState,
	}, nil
}

type FlatStateDB struct {
	*state.StateDB

	flat kvdb.Store
}

func (s *FlatStateDB) SetState(addr common.Address, loc, val common.Hash) {
	s.StateDB.SetState(addr, loc, val)
	s.setState(addr, loc, val)
}

func (s *FlatStateDB) setState(addr common.Address, loc, val common.Hash) {
	key := append(addr.Bytes(), loc.Bytes()...)
	err := s.flat.Put(key, val.Bytes())
	if err != nil {
		panic(err)
	}
}

func (s *FlatStateDB) GetState(addr common.Address, loc common.Hash) common.Hash {
	var empty common.Hash

	key := append(addr.Bytes(), loc.Bytes()...)
	val, err := s.flat.Get(key)
	if err != nil {
		panic(err)
	}

	if val == nil {
		msg := "Forced to get state from trie" // See FillFlatStateCache() note
		value := s.StateDB.GetState(addr, loc)
		s.setState(addr, loc, value)
		if value != empty {
			log.Warn(msg, "reason", "FillFlatStateCache() bad", "addr", addr, "loc", loc.Hex(), "val", value.Hex())
		} else {
			log.Warn(msg, "reason", "reading of non existing key", "addr", addr, "loc", loc.Hex())
		}
		return value
	}

	return common.BytesToHash(val)
}

func (s *Store) FillFlatStateCache(root hash.Hash) error {
	var (
		missingPreimages int
		accounts         uint64
		start            = time.Now()
		logged           = time.Now()
	)

	rootState, err := s.stateDB(root)
	if err != nil {
		panic(err)
	}

	rootTrie, err := s.EvmState.OpenTrie(common.Hash(root))
	if err != nil {
		return err
	}

	log.Info("Flat state cache filling started", "root", rootTrie.Hash())

	it := trie.NewIterator(rootTrie.NodeIterator(nil))
	for it.Next() {
		var data state.Account
		if err = rlp.DecodeBytes(it.Value, &data); err != nil {
			log.Crit("Failed to decode the value returned by iterator", "error", err)
			return err
		}

		addrBytes := rootTrie.GetKey(it.Key)
		if addrBytes == nil {
			// NOTE: No way to iterate by every account.
			// That is why not all the state storage from genesis may be cached.

			missingPreimages++
			continue
		}
		addr := common.BytesToAddress(addrBytes)

		err = rootState.ForEachStorage(addr, func(loc, value common.Hash) bool {
			key := append(addr.Bytes(), loc.Bytes()...)
			err := s.table.FlatState.Put(key, value.Bytes())
			if err != nil {
				panic(err)
			}
			return true
		})
		if err != nil {
			panic(err)
		}

		accounts++
		if time.Since(logged) > 8*time.Second {
			log.Info("Flat state cache filling in progress", "at", common.Bytes2Hex(it.Key), "accounts", accounts,
				"elapsed", common.PrettyDuration(time.Since(start)))
			logged = time.Now()
		}
	}
	if missingPreimages > 0 {
		log.Warn("Dump incomplete due to missing preimages", "missing", missingPreimages)
	}

	log.Info("Flat state cache filling complete", "accounts", accounts,
		"elapsed", common.PrettyDuration(time.Since(start)))
	return nil
}
