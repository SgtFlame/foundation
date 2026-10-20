package impl

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/kb/services/kbservice"
)

func (s *service) OpenVaultAsync(args kbservice.OpenVaultArgs) *kbservice.OpenVaultRequest {
	req := &kbservice.OpenVaultRequest{Response: make(chan *kbservice.OpenVaultResponse, 1)}
	go func() {
		resp := s.OpenVault(args)
		req.Response <- &resp
	}()
	return req
}

// OpenVault resolves args.Path under the service root and returns the ID of the
// vault directory there, assigning a new ID the first time a directory is opened.
func (s *service) OpenVault(args kbservice.OpenVaultArgs) kbservice.OpenVaultResponse {
	path := args.Path.Root(s.root)
	dir, err := path.Build()
	if err != nil {
		return openVaultError(http.StatusBadRequest, err.Error())
	}

	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return openVaultError(http.StatusNotFound, fmt.Sprintf("vault %q does not exist", dir))
	case err != nil:
		return openVaultError(http.StatusInternalServerError, err.Error())
	case !info.IsDir():
		return openVaultError(http.StatusBadRequest, fmt.Sprintf("vault %q is not a directory", dir))
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.vaults[dir]
	if !ok {
		s.nextVaultID++
		id = s.nextVaultID
		s.vaults[dir] = id
	}

	return kbservice.OpenVaultResponse{
		Common:  features.ServiceResponse{Code: http.StatusOK},
		VaultID: id,
	}
}

func openVaultError(code uint16, message string) kbservice.OpenVaultResponse {
	return kbservice.OpenVaultResponse{
		Common: features.ServiceResponse{Code: code, Message: message},
	}
}
