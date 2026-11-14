package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// server is the tallymoth under test: a local process, or a Docker container when limits are wanted.
type server struct {
	pub, priv string
	stop      func()
	memMB     func(peak bool) float64
	oomKilled func() bool
}

func startServer(dir string) *server {
	pub, priv := freeAddr(), freeAddr()
	cfg := filepath.Join(dir, "tallymoth.yaml")
	must(os.WriteFile(cfg, []byte(configYAML()), 0o644))
	if *dockerIm == "" {
		return startLocal(dir, cfg, pub, priv)
	}
	return startDocker(dir, cfg, pub, priv)
}

func startLocal(dir, cfg, pub, priv string) *server {
	cmd := exec.Command(*binPath, "serve", "--config", cfg)
	cmd.Env = append(os.Environ(), "TALLYMOTH_DATA_DIR="+dir, "TALLYMOTH_LISTEN_PUBLIC="+pub, "TALLYMOTH_LISTEN_PRIVATE="+priv)
	must(cmd.Start())
	waitHealthy("http://" + pub)
	return &server{pub: pub, priv: priv,
		stop: func() { cmd.Process.Signal(os.Interrupt); cmd.Wait() },
		memMB: func(peak bool) float64 {
			return rssMB(cmd.Process.Pid, map[bool]string{false: "VmRSS", true: "VmHWM"}[peak])
		},
		oomKilled: func() bool { return false }}
}

func startDocker(dir, cfg, pub, priv string) *server {
	name := fmt.Sprintf("tm-bench-%d", os.Getpid())
	args := []string{"run", "-d", "--name", name, "-u", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
		"-p", pub + ":8080", "-p", priv + ":8081",
		"-v", dir + ":/data", "-v", cfg + ":/config/tallymoth.yaml:ro"}
	if *cpus != "" {
		args = append(args, "--cpus", *cpus)
	}
	if *memLimit != "" {
		// swap off, so the limit is real; GOMEMLIMIT at 80% as the deployment guide recommends
		args = append(args, "--memory", *memLimit, "--memory-swap", *memLimit, "-e", "GOMEMLIMIT="+strconv.FormatInt(memBytes(*memLimit)*8/10, 10))
	}
	args = append(args, *dockerIm, "serve", "--config", "/config/tallymoth.yaml")
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		fatal("docker run: " + string(out))
	}
	waitHealthy("http://" + pub)

	var mu sync.Mutex
	var last, peak float64
	done := make(chan struct{})
	go func() { // docker stats is slow (~1 s per call), so sample in the background
		for {
			select {
			case <-done:
				return
			default:
			}
			o, err := exec.Command("docker", "stats", "--no-stream", "--format", "{{.MemUsage}}", name).Output()
			if err == nil {
				if v := parseMem(string(o)); v > 0 {
					mu.Lock()
					last, peak = v, max(peak, v)
					mu.Unlock()
				}
			}
		}
	}()
	time.Sleep(1500 * time.Millisecond)
	return &server{pub: pub, priv: priv,
		stop: func() {
			close(done)
			exec.Command("docker", "stop", "-t", "10", name).Run()
			exec.Command("docker", "rm", "-f", name).Run()
		},
		memMB: func(p bool) float64 {
			mu.Lock()
			defer mu.Unlock()
			if p {
				return peak
			}
			return last
		},
		oomKilled: func() bool {
			o, _ := exec.Command("docker", "inspect", "-f", "{{.State.OOMKilled}}", name).Output()
			return strings.TrimSpace(string(o)) == "true"
		}}
}

// parseMem reads docker's "23.4MiB / 128MiB" and returns the first number in MB.
func parseMem(s string) float64 {
	f := strings.TrimSpace(strings.SplitN(s, "/", 2)[0])
	for suf, mul := range map[string]float64{"GiB": 1024, "MiB": 1, "KiB": 1.0 / 1024, "B": 1.0 / (1 << 20)} {
		if v, ok := strings.CutSuffix(f, suf); ok {
			n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err == nil {
				return n * mul
			}
		}
	}
	return 0
}

func memBytes(s string) int64 {
	s = strings.ToLower(strings.TrimSpace(s))
	mul := int64(1)
	switch {
	case strings.HasSuffix(s, "g"):
		mul, s = 1<<30, strings.TrimSuffix(s, "g")
	case strings.HasSuffix(s, "m"):
		mul, s = 1<<20, strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "k"):
		mul, s = 1<<10, strings.TrimSuffix(s, "k")
	}
	n, err := strconv.ParseInt(s, 10, 64)
	must(err)
	return n * mul
}
