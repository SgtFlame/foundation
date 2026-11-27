package instance

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadConfigAndCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web-1.json")
	if err := os.WriteFile(path, []byte(`{"unit":"web-1","serviceSet":"web","version":"1.2.0","config":{"log-format":"json","drain-timeout":"45s","nats-replicas":3,"services-loopback":false,"tags":["a","b"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Unit != "web-1" || c.ServiceSet != "web" || c.Flags["log-format"] != "json" || c.Flags["nats-replicas"] != "3" || c.Flags["services-loopback"] != "false" || c.Flags["tags"] != "a,b" {
		t.Fatalf("config %+v", c)
	}
	if names := c.Names(); len(names) != 5 || names[0] != "drain-timeout" {
		t.Fatalf("names %v", names)
	}
	if err := os.WriteFile(path, []byte(`{"unit":"x","config":{"bad":{"nested":1}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("a nested config value was taken")
	}

	creds := t.TempDir()
	for name, v := range map[string]string{"nats.creds": "-----BEGIN NATS USER JWT-----", "nats-urls": "nats://a:4222,nats://b:4222\n", "vault-role-id": "role", "vault-wrapped-secret-id": "wrapped"} {
		if err := os.WriteFile(filepath.Join(creds, name), []byte(v), 0o400); err != nil {
			t.Fatal(err)
		}
	}
	cr := ReadCredentials(creds)
	if cr.NatsCreds != filepath.Join(creds, "nats.creds") || cr.NatsURLs != "nats://a:4222,nats://b:4222" || cr.VaultRoleID != "role" || cr.VaultWrappedSecretID != "wrapped" {
		t.Fatalf("credentials %+v", cr)
	}
	if cr := ReadCredentials(""); cr != (Credentials{}) {
		t.Fatalf("no directory: %+v", cr)
	}
}

func TestNotify(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "notify")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	t.Setenv("NOTIFY_SOCKET", sock)
	if err := Notify("READY=1"); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "READY=1" {
		t.Fatalf("read %q %v", buf[:n], err)
	}
	t.Setenv("NOTIFY_SOCKET", "")
	if err := Notify("READY=1"); err != nil {
		t.Fatal(err)
	}
}
