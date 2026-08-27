package tunnel

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/masteralanlab/free-proxy/internal/config"
)

// exitedManaged builds a Managed around a process that has already exited,
// standing in for a tunnel OpenVPN dropped on its own.
func exitedManaged(t *testing.T, configPath string) *Managed {
	t.Helper()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	cmd := exec.Command("sh", "-c", "exit 0")
	cmd.Stdout, cmd.Stderr = pw, pw
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	_ = pw.Close()

	m := newManaged(cmd, pr, configPath, "fpxtest0", newRing(10))
	deadline := time.After(5 * time.Second)
	for m.Running() {
		select {
		case <-deadline:
			t.Fatal("process did not exit")
		case <-time.After(5 * time.Millisecond):
		}
	}
	return m
}

// Dropping the reference to an exited tunnel has to release what the process
// left behind. Without this the generated .ovpn file and the pipe fd leaked once
// per unexpected exit — which is exactly the path a flapping node repeats.
func TestClearExitedProcessRemovesConfigAndClosesPipe(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, TunnelInterface: "fpxtest0", ProbeDevicePrefix: "fpxtest"}
	mgr := NewManager(cfg)

	configPath := filepath.Join(dir, "active-test.ovpn")
	if err := os.WriteFile(configPath, []byte("remote 198.51.100.1 1194 tcp\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	managed := exitedManaged(t, configPath)

	mgr.mu.Lock()
	mgr.active, mgr.activeNodeID = managed, "jp-abc123"
	mgr.mu.Unlock()

	mgr.ClearExitedProcess()

	if mgr.ActiveNodeID() != "" {
		t.Errorf("active node id survived: %q", mgr.ActiveNodeID())
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf("config file was not removed (stat err: %v)", err)
	}
	// A second close would fail on an already-closed handle; the first must have
	// succeeded.
	if err := managed.pr.Close(); err == nil {
		t.Error("output pipe was still open")
	}
}

// A tunnel that is still running is not the caller's to clear.
func TestClearExitedProcessLeavesRunningTunnelAlone(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir, TunnelInterface: "fpxtest0", ProbeDevicePrefix: "fpxtest"}
	mgr := NewManager(cfg)

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	cmd := exec.Command("sh", "-c", "sleep 30")
	cmd.Stdout, cmd.Stderr = pw, pw
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	_ = pw.Close()

	configPath := filepath.Join(dir, "active-running.ovpn")
	if err := os.WriteFile(configPath, []byte("remote 198.51.100.2 1194 tcp\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	managed := newManaged(cmd, pr, configPath, "fpxtest0", newRing(10))
	t.Cleanup(managed.Stop)

	mgr.mu.Lock()
	mgr.active, mgr.activeNodeID = managed, "jp-running"
	mgr.mu.Unlock()

	mgr.ClearExitedProcess()

	if mgr.ActiveNodeID() != "jp-running" {
		t.Errorf("running tunnel was cleared, active id is %q", mgr.ActiveNodeID())
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("config file of a running tunnel was removed: %v", err)
	}
}
