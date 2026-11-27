package agent

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Health runs each unit's checks whose interval has passed (30 s by
// default) and gives every unit's status: up, down (with what failed)
// or unknown before its first check. A unit with no checks is up while
// systemd says it is active.
func (a *Agent) Health(ctx context.Context) map[string]string {
	a.hmu.Lock()
	units := a.want
	if a.health == nil {
		a.health = map[string]checked{}
	}
	a.hmu.Unlock()
	out := map[string]string{}
	for _, u := range units {
		if u.Name == "" || u.Status == "stopped" {
			continue
		}
		checks := u.Health
		if len(checks) == 0 {
			checks = []Check{{Kind: "unit"}}
		}
		status := "up"
		for i, c := range checks {
			key := u.Name + "#" + strconv.Itoa(i)
			every := time.Duration(c.IntervalSeconds) * time.Second
			if every <= 0 {
				every = 30 * time.Second
			}
			a.hmu.Lock()
			last, ok := a.health[key]
			a.hmu.Unlock()
			if !ok || time.Since(last.at) >= every {
				last = checked{at: time.Now(), status: a.check(ctx, u, c)}
				a.hmu.Lock()
				a.health[key] = last
				a.hmu.Unlock()
			}
			if last.status != "up" {
				status = last.status
				break
			}
		}
		out[u.Name] = status
	}
	return out
}

func (a *Agent) check(ctx context.Context, u Unit, c Check) string {
	host := c.Host
	if host == "" {
		host = "127.0.0.1"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(c.Port))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	switch c.Kind {
	case "unit":
		if a.active(ctx, u.Name) {
			return "up"
		}
		return "down: not active"
	case "tcp", "nats":
		var d net.Dialer
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return "down: " + c.Kind + " " + addr + " does not answer"
		}
		defer conn.Close()
		if c.Kind == "nats" {
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			line, err := bufio.NewReader(conn).ReadString('\n')
			if err != nil || !strings.HasPrefix(line, "INFO ") {
				return "down: nats " + addr + " does not greet"
			}
		}
		return "up"
	case "http":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
		if err != nil {
			return "down: bad url"
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return "down: http " + c.URL + " does not answer"
		}
		res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode > 299 {
			return "down: http " + strconv.Itoa(res.StatusCode)
		}
		return "up"
	}
	return "down: unknown check " + c.Kind
}
