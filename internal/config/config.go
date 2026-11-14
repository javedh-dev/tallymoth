// Package config loads and validates tallymoth.yaml and answers "which tenant owns this key?".
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const Version = 1 // config format version

type Config struct {
	Server  Server
	Tenants []*Tenant

	byID    map[string]*Tenant
	signed  map[string]signedKey // key id -> tenant + key
	bearer  map[[32]byte]*Tenant
	readers map[[32]byte]*Tenant
}

type Server struct {
	Listen      struct{ Public, Private string }
	DataDir     string `yaml:"data_dir"`
	TrustProxy  bool   `yaml:"trust_proxy"`
	TrustedHops int    `yaml:"trusted_hops"`
}

type Tenant struct {
	ID   string
	Auth struct {
		Mode    string // "signed" or "bearer"
		PowBits int    `yaml:"pow_bits"`
	}
	Keys          []Key
	ReadKeys      []Key `yaml:"read_keys"`
	Origins       []string
	Schema        string // "" = strict (default), "open" = anything within caps (bearer only)
	Events        map[string][]string
	Limits        Limits
	RetentionDays int `yaml:"retention_days"` // 0 = keep forever
}

type Limits struct {
	MaxBodyBytes   int `yaml:"max_body_bytes"`
	MaxBatchEvents int `yaml:"max_batch_events"`
	IPPerMinute    int `yaml:"ip_per_minute"`
	DailyCap       int `yaml:"daily_cap"`
	MaxDBMB        int `yaml:"max_db_mb"`
}

// Key: signed keys need ID + Secret. Bearer/read keys need SHA256 (hex) or Secret.
type Key struct {
	ID     string
	Secret string
	SHA256 string
	Status string // "active" (default) or "revoked"
}

type signedKey struct {
	Tenant *Tenant
	Key    *Key
}

// file is the YAML shape; tenants stay raw so each one can be layered over defaults.
type file struct {
	Version  int
	Server   Server
	Defaults Tenant
	Tenants  []yaml.Node
}

// NameRe and PropRe are the allowed event names and prop keys everywhere (config, ingest, query).
var (
	NameRe = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,47}$`)
	PropRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

	idRe    = regexp.MustCompile(`^[a-z0-9-]{2,32}$`)
	envVar  = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	keyIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
)

func builtinDefaults() Tenant {
	var t Tenant
	t.Auth.Mode, t.Auth.PowBits = "signed", 16
	t.Limits = Limits{MaxBodyBytes: 32768, MaxBatchEvents: 50, IPPerMinute: 30, DailyCap: 1_000_000, MaxDBMB: 2048}
	t.RetentionDays = 90
	return t
}

// Load reads path, expands ${VAR}, applies defaults and TALLYMOTH_* env overrides, then validates.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

func Parse(raw []byte) (*Config, error) {
	var missing []string
	raw = envVar.ReplaceAllFunc(raw, func(m []byte) []byte {
		name := string(envVar.FindSubmatch(m)[1])
		v, ok := os.LookupEnv(name)
		if !ok {
			missing = append(missing, name)
		}
		return []byte(v)
	})
	if len(missing) > 0 {
		return nil, fmt.Errorf("environment variable(s) not set: %s", strings.Join(missing, ", "))
	}

	f := file{Defaults: builtinDefaults()}
	f.Server.Listen.Public, f.Server.Listen.Private = ":8080", ":8081"
	f.Server.DataDir, f.Server.TrustedHops = "/data", 1
	if err := strictDecode(raw, &f); err != nil {
		return nil, err
	}
	if f.Version != Version {
		return nil, fmt.Errorf("config: version must be %d (got %d)", Version, f.Version)
	}
	envOverrides(&f.Server)

	c := &Config{Server: f.Server}
	for i := range f.Tenants {
		t := f.Defaults // copy; the tenant block overrides field by field
		t.ID, t.Keys, t.ReadKeys, t.Origins, t.Events = "", nil, nil, nil, nil
		b, err := yaml.Marshal(&f.Tenants[i])
		if err != nil {
			return nil, err
		}
		if err := strictDecode(b, &t); err != nil {
			return nil, fmt.Errorf("tenants[%d]: %w", i, err)
		}
		c.Tenants = append(c.Tenants, &t)
	}
	if err := c.index(); err != nil {
		return nil, err
	}
	return c, nil
}

func strictDecode(b []byte, out any) error {
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true) // a typo must be an error, not a silent default
	return d.Decode(out)
}

func envOverrides(s *Server) {
	if v := os.Getenv("TALLYMOTH_LISTEN_PUBLIC"); v != "" {
		s.Listen.Public = v
	}
	if v := os.Getenv("TALLYMOTH_LISTEN_PRIVATE"); v != "" {
		s.Listen.Private = v
	}
	if v := os.Getenv("TALLYMOTH_DATA_DIR"); v != "" {
		s.DataDir = v
	}
	if v := os.Getenv("TALLYMOTH_TRUST_PROXY"); v != "" {
		s.TrustProxy = v == "1" || v == "true"
	}
	if n, err := strconv.Atoi(os.Getenv("TALLYMOTH_TRUSTED_HOPS")); err == nil {
		s.TrustedHops = n
	}
}

// index validates every tenant and builds the key lookup tables.
func (c *Config) index() error {
	c.byID = map[string]*Tenant{}
	c.signed = map[string]signedKey{}
	c.bearer = map[[32]byte]*Tenant{}
	c.readers = map[[32]byte]*Tenant{}
	keyIDs := map[string]bool{}

	for _, t := range c.Tenants {
		if !idRe.MatchString(t.ID) {
			return fmt.Errorf("tenant id %q must match %s", t.ID, idRe)
		}
		if c.byID[t.ID] != nil {
			return fmt.Errorf("tenant %q defined twice", t.ID)
		}
		c.byID[t.ID] = t
		if err := t.validate(); err != nil {
			return fmt.Errorf("tenant %q: %w", t.ID, err)
		}
		for i := range t.Keys {
			k := &t.Keys[i]
			if k.ID != "" && keyIDs[k.ID] {
				return fmt.Errorf("tenant %q: key id %q is used twice (key ids must be unique across tenants)", t.ID, k.ID)
			}
			keyIDs[k.ID] = true
			if k.Status == "revoked" {
				continue
			}
			if t.Auth.Mode == "signed" {
				c.signed[k.ID] = signedKey{t, k}
			} else {
				if c.bearer[k.hash()] != nil {
					return fmt.Errorf("tenant %q: a write key is also used by another tenant", t.ID)
				}
				c.bearer[k.hash()] = t
			}
		}
		for i := range t.ReadKeys {
			if t.ReadKeys[i].Status != "revoked" {
				c.readers[t.ReadKeys[i].hash()] = t
			}
		}
	}
	return nil
}

func (t *Tenant) validate() error {
	switch t.Auth.Mode {
	case "signed":
		if t.Schema == "open" {
			return fmt.Errorf("signed tenants must use a strict schema (remove schema: open)")
		}
		if t.Auth.PowBits < 8 || t.Auth.PowBits > 28 {
			return fmt.Errorf("auth.pow_bits must be 8..28")
		}
		for _, k := range t.Keys {
			if !keyIDRe.MatchString(k.ID) || k.Secret == "" {
				return fmt.Errorf("signed keys need id (%s) and secret", keyIDRe)
			}
		}
	case "bearer":
		for _, k := range t.Keys {
			if err := k.checkHash(); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("auth.mode must be signed or bearer")
	}
	for _, k := range t.ReadKeys {
		if err := k.checkHash(); err != nil {
			return fmt.Errorf("read_keys: %w", err)
		}
	}
	if t.Schema != "" && t.Schema != "open" {
		return fmt.Errorf("schema must be empty (strict) or \"open\"")
	}
	if t.Schema == "" && len(t.Events) == 0 {
		return fmt.Errorf("strict schema needs an events: allowlist (nothing would be stored)")
	}
	for name, props := range t.Events {
		if !NameRe.MatchString(name) {
			return fmt.Errorf("event name %q must match %s", name, NameRe)
		}
		for _, p := range props {
			if !PropRe.MatchString(p) {
				return fmt.Errorf("event %q: prop %q must match %s", name, p, PropRe)
			}
		}
	}
	if l := t.Limits; l.MaxBodyBytes < 1 || l.MaxBatchEvents < 1 || l.MaxBatchEvents > 500 || l.IPPerMinute < 1 || l.DailyCap < 1 || l.MaxDBMB < 1 {
		return fmt.Errorf("limits must be positive (max_batch_events <= 500)")
	}
	return nil
}

func (k Key) checkHash() error {
	if k.Secret == "" && len(k.SHA256) != 64 {
		return fmt.Errorf("key %q needs secret or a 64-char sha256", k.ID)
	}
	return nil
}

func (k Key) hash() [32]byte {
	if k.SHA256 != "" {
		var h [32]byte
		b, _ := hex.DecodeString(k.SHA256)
		copy(h[:], b)
		return h
	}
	return sha256.Sum256([]byte(k.Secret))
}

// --- lookups used at request time ---

func (c *Config) Tenant(id string) *Tenant { return c.byID[id] }

// SignedKey returns the tenant and key for an X-TM-Key id (nil if unknown or revoked).
func (c *Config) SignedKey(id string) (*Tenant, *Key) {
	s := c.signed[id]
	return s.Tenant, s.Key
}

// Bearer returns the tenant owning a write secret (nil if none).
func (c *Config) Bearer(secret string) *Tenant { return c.bearer[sha256.Sum256([]byte(secret))] }

// ReadKey returns the tenant owning a read secret (nil if none).
func (c *Config) ReadKey(secret string) *Tenant { return c.readers[sha256.Sum256([]byte(secret))] }

// OriginAllowed is used for CORS preflight, which carries no key: allowed if any tenant lists the origin.
func (c *Config) OriginAllowed(origin string) bool {
	for _, t := range c.Tenants {
		if t.AllowsOrigin(origin) {
			return true
		}
	}
	return false
}

func (t *Tenant) AllowsOrigin(origin string) bool {
	for _, o := range t.Origins {
		if o == origin {
			return true
		}
	}
	return false
}
