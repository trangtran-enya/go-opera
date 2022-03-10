package evmstore

import (
	"github.com/ethereum/go-ethereum/common"
)

// journalEntry is a modification entry in the state change journal that can be
// reverted on demand.
type journalEntry interface {
	// revert undoes the changes introduced by this journal entry.
	revert(*FlatStateDB)
}

// journal contains the list of state modifications applied since the last state
// commit. These are tracked to be able to be reverted in case of an execution
// exception or revertal request.
type journal struct {
	entries []journalEntry // Current changes tracked by the journal
}

// newJournal create a new initialized journal.
func newJournal() *journal {
	return &journal{}
}

// append inserts a new modification entry to the end of the change journal.
func (j *journal) append(entry journalEntry) {
	j.entries = append(j.entries, entry)
}

// revert undoes a batch of journalled modifications along with any reverted
// dirty handling too.
func (j *journal) revert(statedb *FlatStateDB, snapshot int) {
	for i := len(j.entries) - 1; i >= snapshot; i-- {
		// Undo the changes made by the operation
		j.entries[i].revert(statedb)
	}
	j.entries = j.entries[:snapshot]
}

// length returns the current number of entries in the journal.
func (j *journal) length() int {
	return len(j.entries)
}

type (
	// Changes to individual accounts.
	storageChange struct {
		account       *common.Address
		key, prevalue common.Hash
	}
)

func (ch storageChange) revert(s *FlatStateDB) {
	s.setState(*ch.account, ch.key, ch.prevalue)
}
