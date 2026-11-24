package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Cycl0o0/OpenDeezer/v3/internal/control"
)

// The Web Remote screen shows the server's live code; Enter after the code was used mints a
// fresh one and keeps the paired phone's session.
func TestWebRemoteFreshCodeKeepsSession(t *testing.T) {
	srv := control.New(control.Config{Addr: "127.0.0.1:0", WebRemote: true},
		func() control.State { return control.State{State: "stopped"} }, nil, control.Commands{}, nil)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	code := srv.EnablePairing()
	base := "http://" + srv.Addr()
	body, _ := json.Marshal(map[string]string{"code": code})
	resp, err := http.Post(base+"/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var pair map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&pair)
	resp.Body.Close()
	if pair["token"] == "" {
		t.Fatalf("pairing failed: %d", resp.StatusCode)
	}

	m := interactionModel(screenWebRemote)
	m.webRemoteSrv, m.webRemoteActive = srv, true
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*Model)
	if cmd != nil {
		next, _ = m.Update(cmd())
		m = next.(*Model)
	}
	if fresh := srv.PairingCode(); fresh == "" || fresh == code || !m.webRemoteActive {
		t.Fatalf("after Enter: code %q (was %q), active %v", fresh, code, m.webRemoteActive)
	}
	req, _ := http.NewRequest(http.MethodGet, base+"/status", nil)
	req.Header.Set("X-OpenDeezer-Session", pair["token"])
	st, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	st.Body.Close()
	if st.StatusCode != http.StatusOK {
		t.Fatalf("the paired session answered %d after the fresh code", st.StatusCode)
	}
}
