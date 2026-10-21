package features

import (
	"math/rand/v2"
	"sync"
	"time"
)

// tsidEpoch is the zero time of TSIDs.
var tsidEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

const tsidRandomBits = 22

var (
	tsidMu   sync.Mutex
	tsidLast uint64
)

// NewTSID returns a time-sorted ID: milliseconds since tsidEpoch in the high
// 42 bits and random low bits, strictly increasing within this process.
func NewTSID() uint64 {
	ms := uint64(time.Since(tsidEpoch).Milliseconds())
	id := ms<<tsidRandomBits | rand.Uint64N(1<<tsidRandomBits)

	tsidMu.Lock()
	defer tsidMu.Unlock()
	if id <= tsidLast {
		id = tsidLast + 1
	}
	tsidLast = id
	return id
}
