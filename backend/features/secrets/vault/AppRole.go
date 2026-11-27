package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// AppRole administration (forge 20h): the credential issuer makes a role
// per service set with its policy, reads its RoleID, and asks for
// response-wrapped, single-use SecretIDs it never sees.

// PutPolicy writes an ACL policy.
func (c *Client) PutPolicy(ctx context.Context, name, policy string) error {
	return c.do(ctx, http.MethodPut, "sys/policies/acl/"+url.PathEscape(name), "", map[string]string{"policy": policy}, nil)
}

// PutAppRole makes or updates an AppRole whose SecretIDs are single use.
func (c *Client) PutAppRole(ctx context.Context, mount, role string, policies []string, tokenTTL time.Duration) error {
	return c.do(ctx, http.MethodPost, "auth/"+mount+"/role/"+url.PathEscape(role), "", map[string]any{
		"token_policies": policies, "secret_id_num_uses": 1, "token_ttl": fmt.Sprintf("%ds", int(tokenTTL.Seconds())),
	}, nil)
}

// RoleID reads an AppRole's RoleID (not secret).
func (c *Client) RoleID(ctx context.Context, mount, role string) (string, error) {
	var out struct {
		Data struct {
			RoleID string `json:"role_id"`
		}
	}
	err := c.do(ctx, http.MethodGet, "auth/"+mount+"/role/"+url.PathEscape(role)+"/role-id", "", nil, &out)
	return out.Data.RoleID, err
}

// Wrapped is a response-wrapping token and the accessor of what it wraps
// (a SecretID's accessor, by which it is destroyed).
type Wrapped struct {
	Token           string `json:"token"`
	Accessor        string `json:"accessor"`
	TTL             int    `json:"ttl"`
	CreationPath    string `json:"creation_path"`
	WrappedAccessor string `json:"wrapped_accessor"`
}

// WrappedSecretID asks for a new SecretID of the role, wrapped for ttl:
// the caller gets only the wrapping token.
func (c *Client) WrappedSecretID(ctx context.Context, mount, role string, ttl time.Duration, metadata map[string]string) (Wrapped, error) {
	var out struct {
		WrapInfo Wrapped `json:"wrap_info"`
	}
	body := map[string]any{}
	if len(metadata) > 0 {
		raw, _ := json.Marshal(metadata)
		body["metadata"] = string(raw)
	}
	err := c.doH(ctx, http.MethodPost, "auth/"+mount+"/role/"+url.PathEscape(role)+"/secret-id", "",
		map[string]string{"X-Vault-Wrap-TTL": fmt.Sprintf("%ds", int(ttl.Seconds()))}, body, &out)
	if err == nil && out.WrapInfo.Token == "" {
		err = fmt.Errorf("vault: the SecretID came back unwrapped")
	}
	return out.WrapInfo, err
}

// DestroySecretID destroys a SecretID by its accessor; one already gone
// (used, expired or destroyed) is not an error.
func (c *Client) DestroySecretID(ctx context.Context, mount, role, accessor string) error {
	err := c.do(ctx, http.MethodPost, "auth/"+mount+"/role/"+url.PathEscape(role)+"/secret-id-accessor/destroy", "", map[string]string{"secret_id_accessor": accessor}, nil)
	if err == ErrNotFound {
		return nil
	}
	if e, ok := err.(*Error); ok && (e.Status == http.StatusBadRequest || e.Status == http.StatusNotFound) {
		return nil
	}
	return err
}
