// Package lxdtest is a fake LXD server on a Unix socket (forge 20c-2):
// instances, profiles, snapshots and operations kept in memory, with
// failure injection, for the control plane's tests.
package lxdtest

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SgtFlame/foundation/backend/features/controlplane/lxd"
)

// Server is the fake.
type Server struct {
	Socket string
	// Driver is what /1.0 reports ("lxc | qemu" by default), Clustered
	// and Name the cluster membership.
	Driver    string
	Clustered bool
	Name      string

	mu        sync.Mutex
	instances map[string]*instance
	profiles  map[string]lxd.Profile
	ops       map[string]*op
	n         int
	creates   int
	targets   []string
	// FailCreate makes the next n creates fail; SlowCreate makes each
	// create's operation take that long.
	FailCreate int
	SlowCreate time.Duration
	// Crash, when set, is called after a create's instance exists but
	// before its operation ends (a daemon killed mid-create).
	Crash func()
}

type instance struct {
	lxd.Instance
	snapshots []string
}

type op struct {
	done chan struct{}
	err  string
}

// Start serves on a socket in a temporary directory until the test ends.
func Start(t testing.TB) *Server {
	t.Helper()
	dir, err := os.MkdirTemp("", "lxd")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Socket: filepath.Join(dir, "unix.socket"), Driver: "lxc | qemu", Name: "none",
		instances: map[string]*instance{}, profiles: map[string]lxd.Profile{"default": {Name: "default", Config: map[string]string{}, Devices: map[string]map[string]string{}}}, ops: map[string]*op{}}
	l, err := net.Listen("unix", s.Socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(s.serve)}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close(); _ = os.RemoveAll(dir) })
	return s
}

// Set changes its settings while it serves.
func (s *Server) Set(fn func(*Server)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(s)
}

// Client is a client of it.
func (s *Server) Client() *lxd.Client { return lxd.New(s.Socket) }

// Instances is a copy of its instances by name.
func (s *Server) Instances() map[string]lxd.Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]lxd.Instance{}
	for n, i := range s.instances {
		c := i.Instance
		c.Config = map[string]string{}
		for k, v := range i.Config {
			c.Config[k] = v
		}
		out[n] = c
	}
	return out
}

// Profiles is a copy of its profiles.
func (s *Server) Profiles() map[string]lxd.Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]lxd.Profile{}
	for n, p := range s.profiles {
		out[n] = p
	}
	return out
}

// Snapshots of an instance.
func (s *Server) Snapshots(name string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.instances[name]; i != nil {
		return append([]string(nil), i.snapshots...)
	}
	return nil
}

// Creates counts the create requests it accepted; Targets the targets they named.
func (s *Server) Creates() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.creates
}

func (s *Server) Targets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.targets...)
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func sync200(w http.ResponseWriter, meta any) {
	write(w, 200, map[string]any{"type": "sync", "status": "Success", "status_code": 200, "metadata": meta})
}

func fail(w http.ResponseWriter, code int, msg string) {
	write(w, code, map[string]any{"type": "error", "error_code": code, "error": msg})
}

// async starts an operation running fn after delay; fn's error fails it.
func (s *Server) async(w http.ResponseWriter, delay time.Duration, fn func() string) {
	s.n++
	id := fmt.Sprintf("op%d", s.n)
	o := &op{done: make(chan struct{})}
	s.ops[id] = o
	go func() {
		if delay > 0 {
			time.Sleep(delay)
		}
		s.mu.Lock()
		o.err = fn()
		s.mu.Unlock()
		close(o.done)
	}()
	write(w, 202, map[string]any{"type": "async", "status": "Operation created", "status_code": 100, "operation": "/1.0/operations/" + id, "metadata": map[string]any{"id": id}})
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasPrefix(path, "/1.0/operations/") {
		s.wait(w, r)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case path == "/1.0":
		sync200(w, map[string]any{"api_version": "1.0", "environment": map[string]any{
			"server_version": "6.1", "server_name": s.Name, "server_clustered": s.Clustered, "driver": s.Driver}})
	case path == "/1.0/instances" && r.Method == http.MethodGet:
		out := []lxd.Instance{}
		for _, i := range s.instances {
			out = append(out, i.Instance)
		}
		sync200(w, out)
	case path == "/1.0/instances" && r.Method == http.MethodPost:
		s.create(w, r)
	case strings.HasPrefix(path, "/1.0/instances/"):
		s.instance(w, r, strings.TrimPrefix(path, "/1.0/instances/"))
	case path == "/1.0/profiles" && r.Method == http.MethodPost:
		var p lxd.Profile
		_ = json.NewDecoder(r.Body).Decode(&p)
		if _, ok := s.profiles[p.Name]; ok {
			fail(w, 409, "profile exists")
			return
		}
		s.profiles[p.Name] = p
		sync200(w, nil)
	case strings.HasPrefix(path, "/1.0/profiles/"):
		name := strings.TrimPrefix(path, "/1.0/profiles/")
		p, ok := s.profiles[name]
		if !ok {
			fail(w, 404, "not found")
			return
		}
		if r.Method == http.MethodPut {
			var in lxd.Profile
			_ = json.NewDecoder(r.Body).Decode(&in)
			p.Description, p.Config, p.Devices = in.Description, in.Config, in.Devices
			s.profiles[name] = p
			sync200(w, nil)
			return
		}
		sync200(w, p)
	case path == "/1.0/storage-pools":
		sync200(w, []lxd.StoragePool{{Name: "default", Driver: "zfs", Status: "Created"}})
	case path == "/1.0/networks":
		sync200(w, []lxd.Network{{Name: "lxdbr0", Type: "bridge", Managed: true}})
	default:
		fail(w, 404, "not found")
	}
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var in lxd.InstanceCreate
	_ = json.NewDecoder(r.Body).Decode(&in)
	if _, ok := s.instances[in.Name]; ok {
		fail(w, 409, "Instance \""+in.Name+"\" already exists")
		return
	}
	for _, p := range in.Profiles {
		if _, ok := s.profiles[p]; !ok {
			fail(w, 400, "profile "+p+" not found")
			return
		}
	}
	s.creates++
	s.targets = append(s.targets, r.URL.Query().Get("target"))
	if s.FailCreate > 0 {
		s.FailCreate--
		s.async(w, 0, func() string { return "image download failed" })
		return
	}
	typ := in.Type
	if typ == "" {
		typ = "container"
	}
	status := "Stopped"
	if in.Start {
		status = "Running"
	}
	s.instances[in.Name] = &instance{Instance: lxd.Instance{Name: in.Name, Type: typ, Status: status, Profiles: in.Profiles, Config: in.Config}}
	crash := s.Crash
	s.async(w, s.SlowCreate, func() string { return "" })
	if crash != nil {
		s.Crash = nil
		go crash()
	}
}

func (s *Server) instance(w http.ResponseWriter, r *http.Request, rest string) {
	name, sub, _ := strings.Cut(rest, "/")
	i, ok := s.instances[name]
	if !ok {
		fail(w, 404, "Instance not found")
		return
	}
	switch {
	case sub == "" && r.Method == http.MethodGet:
		sync200(w, i.Instance)
	case sub == "" && r.Method == http.MethodDelete:
		if i.Running() {
			fail(w, 400, "Instance is running")
			return
		}
		delete(s.instances, name)
		s.async(w, 0, func() string { return "" })
	case sub == "" && r.Method == http.MethodPut:
		var in struct {
			Restore string `json:"restore"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		found := false
		for _, sn := range i.snapshots {
			found = found || sn == in.Restore
		}
		if !found {
			fail(w, 404, "snapshot not found")
			return
		}
		s.async(w, 0, func() string { return "" })
	case sub == "state" && r.Method == http.MethodPut:
		var in struct {
			Action string `json:"action"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		switch in.Action {
		case "start", "restart":
			i.Status = "Running"
		case "stop":
			i.Status = "Stopped"
		default:
			fail(w, 400, "bad action")
			return
		}
		s.async(w, 0, func() string { return "" })
	case sub == "snapshots" && r.Method == http.MethodPost:
		var in struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		for _, sn := range i.snapshots {
			if sn == in.Name {
				fail(w, 409, "snapshot exists")
				return
			}
		}
		i.snapshots = append(i.snapshots, in.Name)
		s.async(w, 0, func() string { return "" })
	case sub == "snapshots":
		out := []string{}
		for _, sn := range i.snapshots {
			out = append(out, "/1.0/instances/"+name+"/snapshots/"+sn)
		}
		sync200(w, out)
	default:
		fail(w, 404, "not found")
	}
}

func (s *Server) wait(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/1.0/operations/"), "/wait")
	s.mu.Lock()
	o := s.ops[id]
	s.mu.Unlock()
	if o == nil {
		fail(w, 404, "operation not found")
		return
	}
	select {
	case <-o.done:
	case <-time.After(10 * time.Second):
		sync200(w, map[string]any{"id": id, "status": "Running"})
		return
	case <-r.Context().Done():
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if o.err != "" {
		sync200(w, map[string]any{"id": id, "status": "Failure", "err": o.err})
		return
	}
	sync200(w, map[string]any{"id": id, "status": "Success"})
}
