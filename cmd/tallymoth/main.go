// tallymoth: privacy-first, multi-tenant event ingest + query.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/javedh-dev/tallymoth/internal/config"
	"github.com/javedh-dev/tallymoth/internal/store"
)

var version = "dev" // set with -ldflags "-X main.version=..."

const usage = `usage: tallymoth <command>
  serve [--config FILE]       run the server (default config: /config/tallymoth.yaml)
  serve --demo                built-in demo tenant with synthetic data, for trying Grafana
  config validate [FILE]      check a config file without starting
  keygen [write|read|signed]  generate a key (default: write)
  healthcheck                 exit 0 if the local server answers /healthz
  version`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch args[0] {
	case "serve":
		path, demo := "/config/tallymoth.yaml", len(args) == 2 && args[1] == "--demo"
		if len(args) == 3 && args[1] == "--config" {
			path = args[2]
		}
		err = serve(path, demo)
	case "config":
		if len(args) < 2 || args[1] != "validate" {
			err = fmt.Errorf("usage: tallymoth config validate [FILE]")
			break
		}
		path := "/config/tallymoth.yaml"
		if len(args) > 2 {
			path = args[2]
		}
		if _, err = config.Load(path); err == nil {
			fmt.Println("config ok")
		}
	case "keygen":
		kind := "write"
		if len(args) > 1 {
			kind = args[1]
		}
		err = keygen(kind)
	case "healthcheck":
		err = healthcheck()
	case "version":
		fmt.Printf("tallymoth %s (commit %s)\ndb schema v%d, protocol /v1, config version %d\n", version, commit(), store.SchemaVersion(), config.Version)
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func commit() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return s.Value[:7]
			}
		}
	}
	return "unknown"
}

func randomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// keygen prints what to paste into the config. Bearer/read secrets are stored hashed; signed secrets must be stored as-is.
func keygen(kind string) error {
	switch kind {
	case "write", "read":
		prefix := map[string]string{"write": "tm_w_", "read": "tm_r_"}[kind]
		secret := prefix + randomString(32)
		sum := sha256.Sum256([]byte(secret))
		fmt.Printf("secret (give to the client, shown once): %s\nconfig:  - {id: %s1, sha256: %s}\n", secret, kind[:1], hex.EncodeToString(sum[:]))
	case "signed":
		fmt.Printf("config:  - {id: k_%s, secret: \"tm_sk_%s\"}\n(the app needs both the id and the secret)\n", randomString(6), randomString(32))
	default:
		return fmt.Errorf("keygen: unknown kind %q (write|read|signed)", kind)
	}
	return nil
}

// healthcheck is for the Docker HEALTHCHECK: the scratch image has no curl.
func healthcheck() error {
	addr := os.Getenv("TALLYMOTH_LISTEN_PUBLIC")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}
