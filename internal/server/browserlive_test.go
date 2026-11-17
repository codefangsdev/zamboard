package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveBrowserWorker(t *testing.T) {
	if os.Getenv("NEXTBOARD_BROWSER_TEST") != "1" {
		t.Skip("set NEXTBOARD_BROWSER_TEST=1 for isolated Chromium integration tests")
	}
	module, err := filepath.Abs("../../web/node_modules/playwright")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(module, "package.json")); err != nil {
		t.Skip("run npm ci in web to test browser worker")
	}
	t.Setenv("NEXTBOARD_PLAYWRIGHT_PATH", module)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<title>Live test</title><input id="name" placeholder="Name"><button id="go" onclick="document.querySelector('#out').textContent='Hello '+document.querySelector('#name').value">Greet</button><p id="out"></p>`))
	}))
	defer site.Close()
	b, err := startLiveBrowser()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = b.input.Close()
		select {
		case <-b.done:
		case <-time.After(10 * time.Second):
			_ = b.cmd.Process.Kill()
			t.Error("worker did not stop")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	call := func(a map[string]any) string {
		t.Helper()
		raw, _ := json.Marshal(a)
		result, err := b.call(ctx, raw)
		if err != nil {
			t.Fatal(err)
		}
		return string(result)
	}
	if result := call(map[string]any{"action": "open", "url": site.URL}); !strings.Contains(result, "Live test") {
		t.Fatal(result)
	}
	call(map[string]any{"action": "fill", "selector": "#name", "text": "Zamboard"})
	if result := call(map[string]any{"action": "click", "selector": "#go"}); !strings.Contains(result, "Hello Zamboard") {
		t.Fatal(result)
	}
	b.frameMu.RLock()
	frame := append([]byte{}, b.frame...)
	b.frameMu.RUnlock()
	img, err := jpeg.DecodeConfig(bytes.NewReader(frame))
	if err != nil || img.Width != 1280 || img.Height != 800 {
		t.Fatalf("live frame: %+v %v", img, err)
	}
	pixels, err := jpeg.Decode(bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	yellow := 0
	for y := 0; y < 120; y++ {
		for x := 0; x < 500; x++ {
			r, g, b, _ := pixels.At(x, y).RGBA()
			if r > 50000 && g > 35000 && b < 30000 {
				yellow++
			}
		}
	}
	if yellow < 10 {
		t.Fatalf("simulated cursor missing from live frame: %d yellow pixels", yellow)
	}
	raw := json.RawMessage(`{"action":"open","url":"file:///etc/passwd"}`)
	if _, err = b.call(ctx, raw); err == nil {
		t.Fatal("file URL accepted")
	}
}

func TestLiveBrowserCanvasAndAuth(t *testing.T) {
	if os.Getenv("NEXTBOARD_BROWSER_TEST") != "1" {
		t.Skip("set NEXTBOARD_BROWSER_TEST=1")
	}
	module, _ := filepath.Abs("../../web/node_modules/playwright")
	t.Setenv("NEXTBOARD_PLAYWRIGHT_PATH", module)
	h := newTestHub(t)
	pid, wid, _ := orchSetup(t, h)
	sid := h.spawnAgent(wid)["id"].(string)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<title>Canvas test</title><button>Test</button>"))
	}))
	defer site.Close()
	target := remoteTarget{sessionID: sid, workspaceID: wid, projectID: pid}
	if status, result := h.do("POST", "/api/browser-test/"+sid+"/open", map[string]string{"url": site.URL}, nil); status != 200 {
		t.Fatalf("manual open HTTP %d: %v", status, result)
	}
	if status, _ := h.do("POST", "/api/browser-test/"+sid+"/open", map[string]string{"url": ""}, nil); status != 400 {
		t.Fatalf("empty URL HTTP %d", status)
	}
	if status, _ := h.do("POST", "/api/browser-test/missing/open", map[string]string{"url": site.URL}, nil); status != 404 {
		t.Fatalf("missing console HTTP %d", status)
	}
	defer h.srv.closeLiveBrowsers()
	doc, err := h.srv.boards.Get(wid)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, n := range doc.Nodes {
		if n.Kind == "browsertest" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("live viewer cards = %d", count)
	}
	if result, failed := h.srv.browserTest(context.Background(), target, json.RawMessage(`{"action":"snapshot"}`)); failed {
		t.Fatal(result)
	}
	doc, _ = h.srv.boards.Get(wid)
	count = 0
	for _, n := range doc.Nodes {
		if n.Kind == "browsertest" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("duplicate live viewer created")
	}
	if _, failed := h.srv.browserTest(context.Background(), remoteTarget{sessionID: "other", workspaceID: wid, projectID: pid}, json.RawMessage(`{"action":"snapshot"}`)); !failed {
		t.Fatal("another console accessed this browser")
	}
	url := h.ts.URL + "/api/browser-test/" + sid + "/frame"
	resp, err := h.c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("frame HTTP %d", resp.StatusCode)
	}
	resp, err = http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("unauthenticated frame HTTP %d", resp.StatusCode)
	}
	if st, _ := h.do("DELETE", "/api/browser-test/"+sid, nil, nil); st != 200 {
		t.Fatalf("stop HTTP %d", st)
	}
}

func TestBrowserToolScope(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target remoteTarget
		want   bool
	}{
		{"console", remoteTarget{sessionID: "s", workspaceID: "w"}, true},
		{"read only", remoteTarget{sessionID: "s", workspaceID: "w", readOnly: true}, false},
		{"runner", remoteTarget{sessionID: "s"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found := false
			for _, tool := range tc.target.tools() {
				found = found || tool["name"] == "browser_test"
			}
			if found != tc.want {
				t.Fatalf("tool visible=%v", found)
			}
		})
	}
}
