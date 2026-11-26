package natsd

import (
	"net"
	"strconv"
)

func splitHostPort(addr string) (string, int, error) {
	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, log.Wrapf(err, "NATS listen address %q", addr)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return "", 0, log.Wrapf(err, "NATS listen port %q", p)
	}
	return host, port, nil
}
