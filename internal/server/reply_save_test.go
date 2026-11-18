package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReplySaveUsesLocalHubForRemoteProject(t *testing.T) {
	h := newTestHub(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	pr, _, err := h.srv.projects.Create("m_offline", "/remote/project", "Remote")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	folder := filepath.Join(home, "Downloads", "NextBoard", pr.ID, "outputs")
	for range 2 {
		w := httptest.NewRecorder()
		h.srv.saveReplyFile(w, httptest.NewRequest("POST", "/", nil), pr.ID, "outputs", "Aruna", "# Hasil\n\nIsi lengkap 日本語", at)
		if w.Code != 200 {
			t.Fatalf("save: %d %s", w.Code, w.Body.String())
		}
	}
	files, err := os.ReadDir(folder)
	if err != nil || len(files) != 2 {
		t.Fatalf("unique local files: %v %v", files, err)
	}
	for _, file := range files {
		b, err := os.ReadFile(filepath.Join(folder, file.Name()))
		if err != nil || !strings.Contains(string(b), "# Hasil\n\nIsi lengkap 日本語") {
			t.Fatalf("content: %s %v", b, err)
		}
	}
}
