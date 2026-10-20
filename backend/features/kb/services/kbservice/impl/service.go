package impl

import (
	"sync"
)

type service struct {
	root string

	mu          sync.Mutex
	vaults      map[string]uint64 // vault directory -> VaultID
	nextVaultID uint64
}
