package relay

import (
	"context"
	"github.com/ledgerwatch/log/v3"
	"os"
)

type Sender struct {
	BinLocation string
}

func NewSender(binLocation string) *Sender {
	return &Sender{
		BinLocation: binLocation,
	}
}

func (s *Sender) Run(ctx context.Context) error {
	_, err := os.ReadFile(s.BinLocation)
	if err != nil {
		log.Error("failed to read tx binary file", "err", err)
	}
	return nil
}
