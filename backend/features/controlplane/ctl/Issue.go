package ctl

import (
	"context"
	"encoding/json"
	"time"
)

// UnitRequest is an agent's: the unit's name and the public key it made
// for the unit's NATS user.
type UnitRequest struct {
	Unit      string `json:"unit"`
	PublicKey string `json:"publicKey"`
}

// UnitReply holds the unit's credentials: a user JWT in Account for the
// key, the set's RoleID and a wrapping token for its SecretID.
type UnitReply struct {
	Account         string    `json:"account,omitempty"`
	JWT             string    `json:"jwt,omitempty"`
	RoleID          string    `json:"roleId,omitempty"`
	WrappedSecretID string    `json:"wrappedSecretId,omitempty"`
	ExpiresAt       time.Time `json:"expiresAt,omitempty"`
	Error           string    `json:"error,omitempty"`
}

// AskIssuer is an agent's call (forge 20h): the unit's credentials for its key.
func (c *Control) AskIssuer(ctx context.Context, instance string, r UnitRequest) (UnitReply, error) {
	raw, _ := json.Marshal(r)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	m, err := c.NC.RequestWithContext(ctx, IssuerSubject(instance), raw)
	if err != nil {
		return UnitReply{}, log.Wrap(err)
	}
	var rep UnitReply
	if err := json.Unmarshal(m.Data, &rep); err != nil {
		return UnitReply{}, log.Wrap(err)
	}
	if rep.Error != "" {
		return rep, log.Code("issue_refused").Errorf("no credentials for unit %s: %s", r.Unit, rep.Error)
	}
	return rep, nil
}
