package server

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/azamrizki/nextboard/internal/auth"
	"github.com/azamrizki/nextboard/internal/board"
	"github.com/azamrizki/nextboard/internal/project"
)

//go:embed browser_test_worker.cjs
var browserWorker string

type liveBrowser struct {
	mu      sync.Mutex
	frameMu sync.RWMutex
	frame   []byte
	cmd     *exec.Cmd
	input   io.WriteCloser
	replies chan browserReply
	done    chan struct{}
	stopped atomic.Bool
}
type browserReply struct {
	Frame  string          `json:"frame"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

func playwrightModule() (string, error) {
	if root := os.Getenv("NEXTBOARD_PLAYWRIGHT_PATH"); root != "" {
		return root, nil
	}
	cwd, _ := os.Getwd()
	exe, _ := os.Executable()
	for _, root := range []string{filepath.Join(cwd, "web", "node_modules", "playwright"), filepath.Join(filepath.Dir(exe), "..", "web", "node_modules", "playwright")} {
		if _, err := os.Stat(filepath.Join(root, "package.json")); err == nil {
			return root, nil
		}
	}
	return "", errors.New("Playwright missing: run cd web && npm ci && npx playwright install chromium; or set NEXTBOARD_PLAYWRIGHT_PATH to the installed playwright package")
}
func startLiveBrowser() (*liveBrowser, error) {
	module, err := playwrightModule()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("node", "-e", browserWorker, module)
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	b := &liveBrowser{cmd: cmd, input: input, replies: make(chan browserReply, 1), done: make(chan struct{})}
	go func() {
		defer close(b.done)
		scan := bufio.NewScanner(output)
		scan.Buffer(make([]byte, 4096), 8<<20)
		for scan.Scan() {
			var reply browserReply
			if json.Unmarshal(scan.Bytes(), &reply) != nil {
				continue
			}
			if reply.Frame != "" {
				if frame, e := base64.StdEncoding.DecodeString(reply.Frame); e == nil {
					b.frameMu.Lock()
					b.frame = frame
					b.frameMu.Unlock()
				}
			} else {
				select {
				case b.replies <- reply:
				case <-b.done:
					return
				}
			}
		}
		_ = input.Close()
		_ = cmd.Wait()
	}()
	return b, nil
}
func (b *liveBrowser) call(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped.Load() {
		return nil, errors.New("browser stopped; open a webpage again")
	}
	if _, err := b.input.Write(append(append([]byte{}, args...), '\n')); err != nil {
		return nil, err
	}
	select {
	case reply := <-b.replies:
		if reply.Error != "" {
			return nil, errors.New(reply.Error)
		}
		return reply.Result, nil
	case <-b.done:
		return nil, errors.New("browser worker stopped; check Node, Playwright and Chromium installation")
	case <-ctx.Done():
		b.stop()
		return nil, ctx.Err()
	}
}
func (b *liveBrowser) stop() {
	if !b.stopped.Swap(true) {
		_ = b.input.Close()
	}
}
func (s *Server) closeLiveBrowsers() {
	s.liveBrowserMu.Lock()
	defer s.liveBrowserMu.Unlock()
	for _, b := range s.liveBrowsers {
		b.stop()
	}
	s.liveBrowsers = nil
}

var browserTestTool = map[string]any{
	"name":        "browser_test",
	"description": "Test a webpage in a live browser visible on the Zamboard canvas, with a simulated cursor. Use open with an HTTP(S) URL, then snapshot for element selectors, click/hover/fill with selector, press with key, scroll with y, or close. One isolated browser per console. This browser runs on the hub, so localhost means the hub. Do not use a separate browser for tests the user wants to watch.",
	"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
		"action": map[string]any{"type": "string", "enum": []string{"open", "snapshot", "click", "hover", "fill", "press", "scroll", "close"}},
		"url":    map[string]any{"type": "string"}, "selector": map[string]any{"type": "string"}, "text": map[string]any{"type": "string"}, "key": map[string]any{"type": "string"}, "y": map[string]any{"type": "number"},
	}, "required": []string{"action"}, "additionalProperties": false},
}

func (s *Server) browserTest(ctx context.Context, t remoteTarget, args json.RawMessage) (string, bool) {
	if t.readOnly || t.sessionID == "" || t.workspaceID == "" {
		return "Browser testing requires a writable agent console on a board", true
	}
	if pr, ok := s.projects.Get(t.projectID); !ok || pr.Sandbox == project.SandboxOffline {
		return "Browser testing is unavailable for this project (missing project or offline sandbox)", true
	}
	var a struct {
		Action string `json:"action"`
	}
	if json.Unmarshal(args, &a) != nil {
		return "Invalid browser arguments", true
	}
	switch a.Action {
	case "open", "snapshot", "click", "hover", "fill", "press", "scroll", "close":
	default:
		return "Unknown browser action", true
	}
	s.liveBrowserMu.Lock()
	if s.liveBrowsers == nil {
		s.liveBrowsers = map[string]*liveBrowser{}
	}
	b := s.liveBrowsers[t.sessionID]
	if a.Action == "close" {
		if b != nil {
			b.stop()
			delete(s.liveBrowsers, t.sessionID)
		}
		s.liveBrowserMu.Unlock()
		return "Browser closed", false
	}
	if b != nil {
		if b.stopped.Load() {
			delete(s.liveBrowsers, t.sessionID)
			b = nil
		}
	}
	if b != nil {
		select {
		case <-b.done:
			delete(s.liveBrowsers, t.sessionID)
			b = nil
		default:
		}
	}
	if b == nil {
		if a.Action != "open" {
			s.liveBrowserMu.Unlock()
			return "Open a webpage first", true
		}
		if len(s.liveBrowsers) >= 8 {
			s.liveBrowserMu.Unlock()
			return "Live browser limit reached (8). Close an unused browser first", true
		}
		var err error
		b, err = startLiveBrowser()
		if err != nil {
			s.liveBrowserMu.Unlock()
			return err.Error(), true
		}
		s.liveBrowsers[t.sessionID] = b
	}
	s.liveBrowserMu.Unlock()
	// Reuse a persistent viewer after a worker restart.
	doc, err := s.boards.Get(t.workspaceID)
	if err != nil {
		return err.Error(), true
	}
	found := false
	for _, n := range doc.Nodes {
		var d struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(n.Data, &d)
		if n.Kind == "browsertest" && d.SessionID == t.sessionID {
			found = true
		}
	}
	if !found {
		data, _ := json.Marshal(map[string]string{"sessionId": t.sessionID})
		x, y := float64(0), float64(0)
		for _, n := range doc.Nodes {
			var d struct {
				SessionID string `json:"sessionId"`
			}
			_ = json.Unmarshal(n.Data, &d)
			if n.Kind == "terminal" && d.SessionID == t.sessionID {
				x = n.X + n.W + 80
				y = n.Y
				break
			}
		}
		_, err = s.boards.Apply(t.workspaceID, "", []board.Op{{Op: "put", Node: &board.Node{ID: auth.RandomID("n_"), Kind: "browsertest", X: x, Y: y, W: 960, H: 640, Data: data}}})
		if err != nil {
			return err.Error(), true
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result, err := b.call(ctx, args)
	if err != nil {
		return err.Error(), true
	}
	return string(result), false
}
func (s *Server) handleBrowserFrame(w http.ResponseWriter, r *http.Request, p *principal) {
	sid := r.PathValue("sid")
	if _, ok := s.sessions.Get(sid); !ok {
		writeErr(w, 404, "console not found")
		return
	}
	s.liveBrowserMu.Lock()
	b := s.liveBrowsers[sid]
	s.liveBrowserMu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	if b == nil {
		writeErr(w, 404, "Browser is not running. Ask the agent to use browser_test with action open.")
		return
	}
	b.frameMu.RLock()
	frame := append([]byte{}, b.frame...)
	b.frameMu.RUnlock()
	if len(frame) == 0 {
		writeErr(w, 503, "Waiting for browser frame")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	_, _ = w.Write(frame)
}

func (s *Server) handleBrowserStop(w http.ResponseWriter, r *http.Request, p *principal) {
	s.liveBrowserMu.Lock()
	if b := s.liveBrowsers[r.PathValue("sid")]; b != nil {
		b.stop()
		delete(s.liveBrowsers, r.PathValue("sid"))
	}
	s.liveBrowserMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleBrowserOpen(w http.ResponseWriter, r *http.Request, p *principal) {
	var req struct {
		URL string `json:"url"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.URL) == 0 || len(req.URL) > 8192 {
		writeErr(w, http.StatusBadRequest, "Webpage URL is required (max 8192 bytes)")
		return
	}
	sess, ok := s.sessions.Get(r.PathValue("sid"))
	if !ok {
		writeErr(w, http.StatusNotFound, "console not found")
		return
	}
	sp := sess.Spec()
	args, _ := json.Marshal(map[string]string{"action": "open", "url": req.URL})
	result, failed := s.browserTest(r.Context(), remoteTarget{sessionID: sp.ID, workspaceID: sp.WorkspaceID, projectID: sp.ProjectID}, args)
	if failed {
		writeErr(w, http.StatusBadRequest, result)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
