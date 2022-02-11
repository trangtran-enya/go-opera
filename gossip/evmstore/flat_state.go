package evmstore

import (
	"fmt"
	"sort"
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
		journal: newJournal(),
	}, nil
}

type FlatStateDB struct {
	*state.StateDB
	flat kvdb.Store
	// Journal of state modifications. This is the backbone of
	// Snapshot and RevertToSnapshot.
	journal        *journal
	validRevisions []revision
}

type revision struct {
	id           int
	journalIndex int
}

// Snapshot returns an identifier for the current revision of the state.
func (s *FlatStateDB) Snapshot() int {
	id := s.StateDB.Snapshot()
	s.validRevisions = append(s.validRevisions, revision{id, s.journal.length()})
	return id
}

// RevertToSnapshot reverts all state changes made since the given revision.
func (s *FlatStateDB) RevertToSnapshot(revid int) {
	// Find the snapshot in the stack of valid snapshots.
	idx := sort.Search(len(s.validRevisions), func(i int) bool {
		return s.validRevisions[i].id >= revid
	})
	if idx == len(s.validRevisions) || s.validRevisions[idx].id != revid {
		panic(fmt.Errorf("revision id %v cannot be reverted", revid))
	}
	snapshot := s.validRevisions[idx].journalIndex

	// Replay the journal to undo changes and remove invalidated snapshots
	s.journal.revert(s, snapshot)
	s.validRevisions = s.validRevisions[:idx]

	s.StateDB.RevertToSnapshot(revid)
}

// IntermediateRoot computes the current root hash of the state trie.
// It is called in between transactions to get the root hash that
// goes into transaction receipts.
func (s *FlatStateDB) IntermediateRoot(deleteEmptyObjects bool) common.Hash {
	s.Finalise(deleteEmptyObjects)
	return s.StateDB.IntermediateRoot(deleteEmptyObjects)
}

// Finalise finalises the state by removing the s destructed objects and clears
// the journal as well as the refunds. Finalise, however, will not push any updates
// into the tries just yet. Only IntermediateRoot or Commit will do that.
func (s *FlatStateDB) Finalise(deleteEmptyObjects bool) {
	if len(s.journal.entries) > 0 {
		s.journal = newJournal()
	}
	s.validRevisions = s.validRevisions[:0] // Snapshots can be created without journal entires

	s.StateDB.Finalise(deleteEmptyObjects)
}

func (s *FlatStateDB) SetState(addr common.Address, loc, val common.Hash) {
	// If the new value is the same as old, don't set
	prev, exists := s.getState(addr, loc)
	if exists && prev == val {
		return
	}
	// New value is different, update and journal the change
	s.journal.append(storageChange{
		account:  &addr,
		key:      loc,
		prevalue: prev,
	})

	s.setState(addr, loc, val)
	s.StateDB.SetState(addr, loc, val)
}

func (s *FlatStateDB) setState(addr common.Address, loc, val common.Hash) {
	key := append(addr.Bytes(), loc.Bytes()...)
	err := s.flat.Put(key, val.Bytes())
	if err != nil {
		panic(err)
	}
}

func (s *FlatStateDB) GetState(addr common.Address, loc common.Hash) common.Hash {
	val, found := s.getState(addr, loc)

	if !found {
		msg := "Forced to get state from trie" // See FillFlatStateCache() note
		value := s.StateDB.GetState(addr, loc)
		s.setState(addr, loc, value)
		if value != emptyHash {
			log.Warn(msg, "reason", "FillFlatStateCache() bad", "addr", addr, "loc", loc.Hex(), "val", value.Hex())
		} else {
			log.Warn(msg, "reason", "reading of non existing key", "addr", addr, "loc", loc.Hex())
		}
		return value
	}

	return val
}

func (s *FlatStateDB) getState(addr common.Address, loc common.Hash) (common.Hash, bool) {
	key := append(addr.Bytes(), loc.Bytes()...)
	val, err := s.flat.Get(key)
	if err != nil {
		panic(err)
	}

	return common.BytesToHash(val), val != nil
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
