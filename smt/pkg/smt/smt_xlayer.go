package smt

import (
	"math/big"
)

func (s *RoSMT) LastHeight() (uint64, error) {
	s.clearUpMutex.Lock()
	defer s.clearUpMutex.Unlock()

	return s.DbRo.GetLastHeight()
}

func (s *SMT) SetLastHeight(newHeight uint64) error {
	s.clearUpMutex.Lock()
	defer s.clearUpMutex.Unlock()

	return s.Db.SetLastHeight(newHeight)
}

// Define the stack entry structure
type stackEntry struct {
	node   *big.Int
	prefix []byte
}
