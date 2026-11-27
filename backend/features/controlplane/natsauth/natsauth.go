// Package natsauth makes the deployment's NATS authorization as
// decentralized JWTs (forge 20a-2, documentation/ControlPlane.md): an
// operator, the system account, the control account that carries ctl.>,
// the application account every tenant shares until forge 20a-3, and
// users with scoped permissions as creds files. No nsc binary: keys come
// from nkeys and claims from nats-io/jwt.
package natsauth

import (
	"fmt"
	"strings"
	"time"

	"github.com/SgtFlame/foundation/backend/features/logging"
	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

var log = logging.In("controlplane").Tags("nats")

// Account names in a deployment.
const (
	System  = "SYS"
	Control = "CTL"
	App     = "APP"
)

// Operator is the deployment's root of trust. Its seed signs accounts;
// forge 20a-2's question 1: kept offline once the accounts exist.
type Operator struct {
	Name string
	Key  nkeys.KeyPair
	// Signing is the operator's signing key (forge 20h), kept online so an
	// account can be signed again (a revocation) while Key is offline.
	Signing nkeys.KeyPair
	JWT     string
}

// Account is one NATS account with a signing key that signs its users,
// so the account's own identity key need not be online.
type Account struct {
	Name    string
	Key     nkeys.KeyPair
	Signing nkeys.KeyPair
	Claims  *jwt.AccountClaims
	JWT     string
}

// Public is the account's public key, its id in NATS.
func (a *Account) Public() string {
	p, _ := a.Key.PublicKey()
	return p
}

// NewOperator makes an operator with a fresh key.
func NewOperator(name string) (*Operator, error) {
	kp, err := nkeys.CreateOperator()
	if err != nil {
		return nil, log.Wrap(err)
	}
	return operatorFrom(name, kp)
}

func operatorFrom(name string, kp nkeys.KeyPair) (*Operator, error) {
	pub, err := kp.PublicKey()
	if err != nil {
		return nil, log.Wrap(err)
	}
	sk, err := nkeys.CreateOperator()
	if err != nil {
		return nil, log.Wrap(err)
	}
	spub, _ := sk.PublicKey()
	oc := jwt.NewOperatorClaims(pub)
	oc.Name = name
	oc.SigningKeys.Add(spub)
	token, err := oc.Encode(kp)
	if err != nil {
		return nil, log.Wrap(err)
	}
	return &Operator{Name: name, Key: kp, Signing: sk, JWT: token}, nil
}

// Limits are an account's JetStream and connection limits; zero values
// are unlimited.
type Limits struct {
	Connections int64
	Storage     int64 // bytes of file storage
	Memory      int64 // bytes of memory storage
	Streams     int64
}

// NewAccount makes an account signed by the operator, with JetStream
// enabled within its limits.
func (o *Operator) NewAccount(name string, limits Limits) (*Account, error) {
	kp, err := nkeys.CreateAccount()
	if err != nil {
		return nil, log.Wrap(err)
	}
	sk, err := nkeys.CreateAccount()
	if err != nil {
		return nil, log.Wrap(err)
	}
	pub, _ := kp.PublicKey()
	ac := jwt.NewAccountClaims(pub)
	ac.Name = name
	spub, _ := sk.PublicKey()
	ac.SigningKeys.Add(spub)
	a := &Account{Name: name, Key: kp, Signing: sk, Claims: ac}
	a.SetLimits(limits)
	if err := o.Sign(a); err != nil {
		return nil, err
	}
	return a, nil
}

// SetLimits changes the account's limits; Sign it again and push it.
func (a *Account) SetLimits(l Limits) {
	unl := func(v int64) int64 {
		if v == 0 {
			return jwt.NoLimit
		}
		return v
	}
	a.Claims.Limits.Conn = unl(l.Connections)
	a.Claims.Limits.JetStreamLimits = jwt.JetStreamLimits{
		DiskStorage: unl(l.Storage), MemoryStorage: unl(l.Memory), Streams: unl(l.Streams),
		Consumer: jwt.NoLimit, MaxAckPending: jwt.NoLimit,
	}
	if a.Name == System { // the system account never has JetStream
		a.Claims.Limits.JetStreamLimits = jwt.JetStreamLimits{}
	}
}

// Revoke revokes a user's key from now on: the account must be signed
// and pushed again for it to take effect.
func (a *Account) Revoke(userPublic string) {
	a.Claims.Revoke(userPublic)
}

// Sign signs the account's current claims with the operator's signing
// key, else its own key.
func (o *Operator) Sign(a *Account) error {
	k := o.Signing
	if k == nil {
		k = o.Key
	}
	if k == nil {
		return log.Code("no_operator_key").Errorf("no operator key to sign account %s", a.Name)
	}
	token, err := a.Claims.Encode(k)
	if err != nil {
		return log.Wrapf(err, "sign account %s", a.Name)
	}
	a.JWT = token
	return nil
}

// Permissions are a user's allowed subjects. Empty lists allow nothing
// for that direction.
type Permissions struct {
	Pub []string
	Sub []string
	// Responses lets the user answer requests sent to it (a daemon's
	// request subjects), with no other publish permission needed.
	Responses bool
}

// Everything allows every subject inside the account.
var Everything = Permissions{Pub: []string{">"}, Sub: []string{">"}}

// User is a minted user: its key and the creds file NATS clients load.
type User struct {
	Name   string
	Public string
	JWT    string
	Creds  []byte
}

// NewUser mints a user of the account with the permissions, signed by the
// account's signing key, expiring after ttl (0: never).
func (a *Account) NewUser(name string, p Permissions, ttl time.Duration) (*User, error) {
	kp, err := nkeys.CreateUser()
	if err != nil {
		return nil, log.Wrap(err)
	}
	pub, _ := kp.PublicKey()
	token, err := a.UserJWT(pub, name, p, ttl)
	if err != nil {
		return nil, err
	}
	seed, err := kp.Seed()
	if err != nil {
		return nil, log.Wrap(err)
	}
	creds, err := jwt.FormatUserConfig(token, seed)
	if err != nil {
		return nil, log.Wrap(err)
	}
	return &User{Name: name, Public: pub, JWT: token, Creds: creds}, nil
}

// UserJWT signs a user JWT for a key someone else holds (forge 20c-1: a
// daemon enrolling sends only its public key, so its seed never leaves
// the machine).
func (a *Account) UserJWT(public, name string, p Permissions, ttl time.Duration) (string, error) {
	if !nkeys.IsValidPublicUserKey(public) {
		return "", log.Code("bad_user_key").Errorf("%q is not a user public key", public)
	}
	uc := jwt.NewUserClaims(public)
	uc.Name = name
	uc.IssuerAccount = a.Public()
	uc.Pub.Allow.Add(p.Pub...)
	uc.Sub.Allow.Add(p.Sub...)
	if len(p.Pub) == 0 {
		uc.Pub.Deny.Add(">")
	}
	if len(p.Sub) == 0 {
		uc.Sub.Deny.Add(">")
	}
	if p.Responses {
		uc.Resp = &jwt.ResponsePermission{MaxMsgs: 1, Expires: time.Minute}
	}
	if ttl > 0 {
		uc.Expires = time.Now().Add(ttl).Unix()
	}
	token, err := uc.Encode(a.Signing)
	return token, log.Wrap(err)
}

// Deployment is the operator and the three accounts of forge 20a-2.
type Deployment struct {
	Operator *Operator
	Accounts map[string]*Account
}

// Init makes a new deployment's operator and its system, control and
// application accounts.
func Init(operator string) (*Deployment, error) {
	op, err := NewOperator(operator)
	if err != nil {
		return nil, err
	}
	d := &Deployment{Operator: op, Accounts: map[string]*Account{}}
	for _, name := range []string{System, Control, App} {
		if _, err := d.AddAccount(name, Limits{}); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// AddAccount makes another account (a tenant's, forge 20a-3).
func (d *Deployment) AddAccount(name string, limits Limits) (*Account, error) {
	if d.Accounts[name] != nil {
		return nil, log.Errorf("account %s exists", name)
	}
	a, err := d.Operator.NewAccount(name, limits)
	if err != nil {
		return nil, err
	}
	d.Accounts[name] = a
	return a, nil
}

// Account is the named account, nil if none.
func (d *Deployment) Account(name string) *Account { return d.Accounts[name] }

// ServerConfig renders the nats-server configuration lines for the trust
// chain: the operator, the system account and the full resolver keeping
// account JWTs in dir, preloaded with every account so the first start
// needs no push. A deployment's nats-server.conf includes it.
func (d *Deployment) ServerConfig(resolverDir string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "operator: %q\n", d.Operator.JWT)
	fmt.Fprintf(&b, "system_account: %q\n", d.Accounts[System].Public())
	fmt.Fprintf(&b, "resolver: {\n  type: full\n  dir: %q\n  allow_delete: false\n  interval: \"2m\"\n}\n", resolverDir)
	b.WriteString("resolver_preload: {\n")
	for _, name := range sortedNames(d.Accounts) {
		a := d.Accounts[name]
		fmt.Fprintf(&b, "  %s: %q\n", a.Public(), a.JWT)
	}
	b.WriteString("}\n")
	return b.String()
}

func sortedNames(m map[string]*Account) []string {
	var names []string
	for n := range m {
		names = append(names, n)
	}
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}
