// Command opendeezer-mcp is an MCP server (JSON-RPC 2.0 over stdio) that drives the
// OpenDeezer client at $OPENDEEZER_CONTROL_URL (default http://127.0.0.1:7654) through
// its control API, authenticated by $OPENDEEZER_CONTROL_TOKEN or _CONTROL_ACCOUNT.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	version_ "github.com/Cycl0o0/OpenDeezer/v3/internal/version"
)

var version = version_.Number

// protocolVersion is the MCP revision this server was written against; the later
// revisions in compatibleProtocolVersions are echoed back when a client asks for them.
const protocolVersion = "2024-11-05"

// compatibleProtocolVersions lists the MCP revisions echoed back on request: they only
// add features this server does not use. Other versions get protocolVersion. Append a
// revision only after checking its changelog against this server.
var compatibleProtocolVersions = map[string]bool{
	"2024-11-05": true,
	"2025-03-26": true,
	"2025-06-18": true,
}

// annotationsMinVersion is the first MCP revision with tool annotations
// (readOnlyHint et al.). Revisions are dates, so a plain string compare works.
const annotationsMinVersion = "2025-03-26"

func main() {
	tgt := newTarget(
		env("OPENDEEZER_CONTROL_URL", "http://127.0.0.1:7654"),
		os.Getenv("OPENDEEZER_CONTROL_TOKEN"),
		os.Getenv("OPENDEEZER_CONTROL_ACCOUNT"),
	)
	s := &server{target: tgt, tools: buildTools(tgt), proto: protocolVersion}
	s.serve(os.Stdin, os.Stdout)
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// ---- JSON-RPC ----

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type server struct {
	target *target
	tools  []tool
	proto  string // negotiated protocol version (set by initialize)
	out    *bufio.Writer
}

// maxLineBytes caps one JSON-RPC request line; a longer line draws a parse error
// and the read loop goes on with the next line.
const maxLineBytes = 8 << 20 // 8 MiB

func (s *server) serve(in io.Reader, out io.Writer) {
	s.out = bufio.NewWriter(out)
	// A bufio.Reader (not a Scanner) lets us cap and *recover* from an oversized
	// line rather than aborting: readLine drains the offending line and reports
	// tooLong so we can answer -32700 and continue with the next request.
	br := bufio.NewReaderSize(in, 64<<10)
	for {
		line, tooLong, err := readLine(br, maxLineBytes)
		if tooLong {
			logf("parse error: request line exceeds %d bytes", maxLineBytes)
			s.replyErr(json.RawMessage("null"), -32700, "Parse error")
		} else if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			var req rpcReq
			if uerr := json.Unmarshal(trimmed, &req); uerr != nil {
				logf("parse error: %v", uerr)
				s.replyErr(json.RawMessage("null"), -32700, "Parse error")
			} else {
				s.dispatch(req)
			}
		}
		if err != nil {
			if err != io.EOF {
				logf("stdin: %v", err)
				os.Exit(1)
			}
			return
		}
	}
}

// readLine reads one line from r, trailing '\n' included. A line over max bytes is
// drained and reported as tooLong with a nil line. A final line without '\n' comes
// back with err == io.EOF.
func readLine(r *bufio.Reader, max int) (line []byte, tooLong bool, err error) {
	for {
		frag, e := r.ReadSlice('\n')
		if !tooLong {
			if len(line)+len(frag) > max {
				tooLong, line = true, nil // over cap: drop and drain the remainder
			} else {
				line = append(line, frag...)
			}
		}
		if e == bufio.ErrBufferFull {
			continue // more of this line remains in the reader
		}
		return line, tooLong, e
	}
}

func (s *server) dispatch(req rpcReq) {
	switch req.Method {
	case "initialize":
		// Echo the client's version when it is compatible, else answer the pinned baseline
		// and let the client decide. serve() runs on one goroutine, so no lock is needed.
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		s.proto = protocolVersion
		if compatibleProtocolVersions[p.ProtocolVersion] {
			s.proto = p.ProtocolVersion
		}
		s.reply(req.ID, map[string]any{
			"protocolVersion": s.proto,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "opendeezer-mcp", "version": version},
		})
	case "tools/list":
		// Tool annotations (readOnlyHint) exist only from 2025-03-26 on; emit
		// them solely when the negotiated revision knows the field, so clients
		// validating against the older schema never see an unknown key.
		withAnnotations := s.proto >= annotationsMinVersion
		s.reply(req.ID, map[string]any{"tools": toolSpecs(s.tools, withAnnotations)})
	case "tools/call":
		s.handleCall(req)
	case "ping":
		s.reply(req.ID, map[string]any{})
	default:
		// Notifications (e.g. notifications/initialized) have no id and need no
		// response; unknown requests get a method-not-found error.
		if len(req.ID) > 0 {
			s.replyErr(req.ID, -32601, "method not found: "+req.Method)
		}
	}
}

func (s *server) handleCall(req rpcReq) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		s.replyErr(req.ID, -32602, "invalid params: "+err.Error())
		return
	}
	for _, t := range s.tools {
		if t.name == p.Name {
			text, err := t.run(p.Arguments)
			if err != nil {
				s.reply(req.ID, toolResult("error: "+err.Error(), true))
				return
			}
			s.reply(req.ID, toolResult(text, false))
			return
		}
	}
	s.reply(req.ID, toolResult("unknown tool: "+p.Name, true))
}

func toolResult(text string, isErr bool) map[string]any {
	r := map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
	if isErr {
		r["isError"] = true
	}
	return r
}

func (s *server) reply(id json.RawMessage, result any) {
	if len(id) == 0 {
		return // notification: no response
	}
	s.write(rpcResp{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *server) replyErr(id json.RawMessage, code int, msg string) {
	if len(id) == 0 {
		return // notification: no response
	}
	s.write(rpcResp{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func (s *server) write(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		logf("marshal error: %v", err)
		return
	}
	_, _ = s.out.Write(b)
	_ = s.out.WriteByte('\n')
	_ = s.out.Flush()
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "opendeezer-mcp: "+format+"\n", args...)
}
