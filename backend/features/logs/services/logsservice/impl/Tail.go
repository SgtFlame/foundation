package impl

import (
	"bufio"
	"encoding/json"
	"net/http"

	"github.com/SgtFlame/foundation/backend/features"
	"github.com/SgtFlame/foundation/backend/features/logs/services/logsservice"
)

// Tail follows new matching records until args' context ends.
func (s *service) Tail(args logsservice.TailArgs) *logsservice.TailRequest {
	rows := make(chan logsservice.Row, 64)
	resp, f := s.startTail(args)
	if f != nil {
		close(rows)
		return &logsservice.TailRequest{Common: *f.resp, QueryError: f.queryError, Rows: rows}
	}
	go func() {
		defer close(rows)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			var row logsservice.Row
			if json.Unmarshal(scanner.Bytes(), &row) != nil {
				continue
			}
			select {
			case rows <- row:
			case <-args.Context().Done():
				return
			}
		}
	}()
	return &logsservice.TailRequest{Common: features.OK(), Rows: rows}
}

func (s *service) startTail(args logsservice.TailArgs) (*http.Response, *failure) {
	p, f := params(args.ServiceArgs, args.Query, nil)
	if f != nil {
		return nil, f
	}
	return s.call(args.ServiceArgs, s.tail, "/select/logsql/tail", p)
}
