package natsauth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// Keys keeps a deployment's seeds and account JWTs. DirKeys keeps them in
// files only their owner reads (`cli nats init`); the credential issuer
// (forge 20h) keeps them in the SecretStore.
type Keys interface {
	Get(name string) ([]byte, error)
	Put(name string, value []byte) error
}

// DirKeys keeps each value in <dir>/<name>, mode 0600.
type DirKeys string

func (d DirKeys) Get(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(string(d), name))
}

func (d DirKeys) Put(name string, value []byte) error {
	if err := os.MkdirAll(string(d), 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(string(d), name), value, 0o600)
}

type savedAccount struct {
	Seed    string `json:"seed"`
	Signing string `json:"signing"`
	JWT     string `json:"jwt"`
}

// Save writes the operator's seed (operator.nk), each account's keys and
// JWT (account-<name>.json) and the account list (accounts.json).
func (d *Deployment) Save(k Keys) error {
	seed, err := d.Operator.Key.Seed()
	if err != nil {
		return log.Wrap(err)
	}
	if err := k.Put("operator.nk", seed); err != nil {
		return log.Wrap(err)
	}
	if err := k.Put("operator.jwt", []byte(d.Operator.JWT)); err != nil {
		return log.Wrap(err)
	}
	names := sortedNames(d.Accounts)
	for _, name := range names {
		if err := d.SaveAccount(k, name); err != nil {
			return err
		}
	}
	b, _ := json.Marshal(names)
	return log.Wrap(k.Put("accounts.json", b))
}

// SaveAccount writes one account after a change (limits, revocations),
// or a new one (a tenant's), adding it to the account list.
func (d *Deployment) SaveAccount(k Keys, name string) error {
	a := d.Accounts[name]
	seed, _ := a.Key.Seed()
	sseed, _ := a.Signing.Seed()
	b, _ := json.Marshal(savedAccount{Seed: string(seed), Signing: string(sseed), JWT: a.JWT})
	if err := k.Put("account-"+name+".json", b); err != nil {
		return log.Wrap(err)
	}
	var names []string
	if raw, err := k.Get("accounts.json"); err == nil {
		_ = json.Unmarshal(raw, &names)
	}
	if slices.Contains(names, name) {
		return nil
	}
	names = append(names, name)
	slices.Sort(names)
	raw, _ := json.Marshal(names)
	return log.Wrap(k.Put("accounts.json", raw))
}

// Load reads a deployment Save wrote. A deployment whose operator seed was
// taken offline loads without it (Operator.Key nil): its accounts can
// still mint users, but not be signed again.
func Load(k Keys) (*Deployment, error) {
	opJWT, err := k.Get("operator.jwt")
	if err != nil {
		return nil, log.Wrapf(err, "read the operator")
	}
	oc, err := jwt.DecodeOperatorClaims(string(opJWT))
	if err != nil {
		return nil, log.Wrap(err)
	}
	d := &Deployment{Operator: &Operator{Name: oc.Name, JWT: string(opJWT)}, Accounts: map[string]*Account{}}
	if seed, err := k.Get("operator.nk"); err == nil {
		if d.Operator.Key, err = nkeys.FromSeed(seed); err != nil {
			return nil, log.Wrap(err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, log.Wrap(err)
	}
	b, err := k.Get("accounts.json")
	if err != nil {
		return nil, log.Wrapf(err, "read the account list")
	}
	var names []string
	if err := json.Unmarshal(b, &names); err != nil {
		return nil, log.Wrap(err)
	}
	for _, name := range names {
		b, err := k.Get("account-" + name + ".json")
		if err != nil {
			return nil, log.Wrapf(err, "read account %s", name)
		}
		var s savedAccount
		if err := json.Unmarshal(b, &s); err != nil {
			return nil, log.Wrap(err)
		}
		a := &Account{Name: name, JWT: s.JWT}
		if a.Key, err = nkeys.FromSeed([]byte(s.Seed)); err != nil {
			return nil, log.Wrap(err)
		}
		if a.Signing, err = nkeys.FromSeed([]byte(s.Signing)); err != nil {
			return nil, log.Wrap(err)
		}
		if a.Claims, err = jwt.DecodeAccountClaims(s.JWT); err != nil {
			return nil, log.Wrap(err)
		}
		d.Accounts[name] = a
	}
	return d, nil
}

// Push sends an account's JWT to every server through the resolver
// ($SYS.REQ.CLAIMS.UPDATE), over a connection of a system account user.
// Accounts change without touching server files or restarting.
func Push(nc *nats.Conn, a *Account) error {
	msg, err := nc.Request("$SYS.REQ.CLAIMS.UPDATE", []byte(a.JWT), 5*time.Second)
	if err != nil {
		return log.Wrapf(err, "push account %s", a.Name)
	}
	var reply struct {
		Data *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"data"`
		Error *struct {
			Code        int    `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	}
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		return log.Wrapf(err, "push account %s", a.Name)
	}
	if reply.Error != nil {
		return log.Errorf("push account %s: %s", a.Name, reply.Error.Description)
	}
	return nil
}
