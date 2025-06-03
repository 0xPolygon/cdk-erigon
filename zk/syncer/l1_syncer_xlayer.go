package syncer

import "time"

func (s *L1Syncer) UpdateConfig(getLogsTimeout time.Duration, getLogsRetries int) {
	s.getLogsTimeout = getLogsTimeout
	s.getLogsRetries = getLogsRetries
}
