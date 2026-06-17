package llamaserver

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/AresiusXP/llama-tui/internal/config"
	"github.com/AresiusXP/llama-tui/internal/hardware"
)

// State represents the current state of llama-server.
type State int

const (
	StateStopped  State = iota
	StateStarting
	StateRunning
	StateError
)

func (s State) String() string {
	switch s {
	case StateStopped:
		return "STOPPED"
	case StateStarting:
		return "STARTING"
	case StateRunning:
		return "RUNNING"
	case StateError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ServerStartedMsg is sent when llama-server becomes healthy.
type ServerStartedMsg struct {
	Model   string
	Address string
}

// ServerStoppedMsg is sent when llama-server exits.
type ServerStoppedMsg struct {
	Err error
}

// ServerLogMsg carries a log line from llama-server's output.
type ServerLogMsg struct {
	Line string
}

// ModelRunConfig is the runtime configuration for a model load.
type ModelRunConfig struct {
	ModelPath      string
	Port           int
	ContextSize    int
	GPULayers      int
	GPU            *hardware.GPU // nil = CPU only

	// Advanced options.
	ParallelSlots  int    // --parallel N; -1 = auto (passed only when != -1)
	KVCacheTypeK   string // --cache-type-k TYPE; empty = omit (use llama-server default)
	KVCacheTypeV   string // --cache-type-v TYPE; empty = omit
	MetricsEnabled bool   // --metrics; expose Prometheus /metrics endpoint

	// Per-model extras.
	Threads   int // --threads N; 0 = omit flag entirely
	BatchSize int // --batch-size N (-b); 0 = omit flag (llama-server default: 2048)
}

const maxLogLines = 200

// Manager manages the llama-server subprocess lifecycle.
type Manager struct {
	mu          sync.Mutex
	cfg         *config.Config
	state       State
	cmd         *exec.Cmd
	cancel      context.CancelFunc
	activeModel string
	activePort  int
	logBuf      []string // ring buffer, max 200 lines
	logMu       sync.Mutex
	program     *tea.Program // for sending msgs back to the TUI

	// waitDone is closed when the current process has fully exited and
	// cmd.Wait() has returned. UnloadModel waits on this instead of calling
	// cmd.Wait() again (which would violate the exec.Cmd contract).
	waitDone chan struct{}
}

// NewManager creates a new Manager.
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		cfg:    cfg,
		state:  StateStopped,
		logBuf: make([]string, 0, maxLogLines),
	}
}

// SetProgram registers the Bubble Tea program to send messages to.
func (m *Manager) SetProgram(p *tea.Program) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.program = p
}

// State returns the current server state.
func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// ActiveModel returns the currently loaded model path (empty if stopped).
func (m *Manager) ActiveModel() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeModel
}

// ActiveAddress returns the server address (e.g. "http://localhost:8080").
func (m *Manager) ActiveAddress() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activePort == 0 {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", m.activePort)
}

// LoadModel stops any running server and starts a new one with the given config.
func (m *Manager) LoadModel(runCfg ModelRunConfig) error {
	// Stop any currently running server first.
	if err := m.UnloadModel(); err != nil {
		return fmt.Errorf("unload existing model: %w", err)
	}

	m.mu.Lock()
	m.state = StateStarting
	m.activeModel = runCfg.ModelPath
	m.activePort = runCfg.Port
	m.mu.Unlock()

	// Build argument list.
	args := []string{
		"--model", runCfg.ModelPath,
		"--port", strconv.Itoa(runCfg.Port),
		"--ctx-size", strconv.Itoa(runCfg.ContextSize),
		"--host", "127.0.0.1",
	}
	args = append(args, hardware.LlamaServerFlags(runCfg.GPU, runCfg.GPULayers)...)

	// Advanced options — only appended when explicitly configured.
	if runCfg.ParallelSlots != -1 {
		args = append(args, "--parallel", strconv.Itoa(runCfg.ParallelSlots))
	}
	if runCfg.KVCacheTypeK != "" {
		args = append(args, "--cache-type-k", runCfg.KVCacheTypeK)
	}
	if runCfg.KVCacheTypeV != "" {
		args = append(args, "--cache-type-v", runCfg.KVCacheTypeV)
	}
	if runCfg.MetricsEnabled {
		args = append(args, "--metrics")
	}
	if runCfg.Threads > 0 {
		args = append(args, "--threads", strconv.Itoa(runCfg.Threads))
	}
	if runCfg.BatchSize > 0 {
		args = append(args, "--batch-size", strconv.Itoa(runCfg.BatchSize))
	}

	ctx, cancel := context.WithCancel(context.Background())

	binaryPath := m.cfg.LlamaServerPath()
	cmd := exec.CommandContext(ctx, binaryPath, args...)

	// Inject GPU-specific environment variables (e.g. GGML_VK_VISIBLE_DEVICES).
	// LlamaServerEnv returns nil for CPU/Apple, so we only set cmd.Env when
	// there is actually something extra to add.
	if extraEnv := hardware.LlamaServerEnv(runCfg.GPU); len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}

	// Pipe stdout and stderr separately — do NOT use io.MultiReader, which
	// reads them sequentially and deadlocks if stderr fills its buffer while
	// stdout is still open. Instead, scan each pipe in its own goroutine.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		m.mu.Lock()
		m.state = StateError
		m.activeModel = ""
		m.activePort = 0
		m.mu.Unlock()
		return fmt.Errorf("start llama-server: %w", err)
	}

	// waitDone is closed once both scanners have finished and Wait() has returned.
	waitDone := make(chan struct{})

	m.mu.Lock()
	m.cmd = cmd
	m.cancel = cancel
	m.waitDone = waitDone
	m.mu.Unlock()

	// scanPipe scans one pipe and writes lines to the log / TUI.
	scanPipe := func(r io.Reader, wg *sync.WaitGroup) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			m.appendLog(line)
			m.send(ServerLogMsg{Line: line})
		}
	}

	var pipeWg sync.WaitGroup
	pipeWg.Add(2)
	go scanPipe(stdout, &pipeWg)
	go scanPipe(stderr, &pipeWg)

	// Goroutine: wait for both pipes to drain, then call Wait().
	// This is the only goroutine that ever calls cmd.Wait().
	go func() {
		defer close(waitDone)
		pipeWg.Wait() // all pipe data drained — safe to call Wait now
		err := cmd.Wait()
		// Cancel the context so waitForHealth returns immediately instead of
		// continuing to poll for up to 60 seconds after the process has exited.
		if cancel != nil {
			cancel()
		}
		m.mu.Lock()
		m.state = StateStopped
		m.activeModel = ""
		m.activePort = 0
		m.mu.Unlock()
		m.send(ServerStoppedMsg{Err: err})
	}()

	// Goroutine: poll for health and send started message.
	go func() {
		if waitForHealth(ctx, runCfg.Port) {
			m.mu.Lock()
			// Only transition to Running if we're still in Starting state.
			// (UnloadModel may have been called while we were polling.)
			if m.state == StateStarting {
				m.state = StateRunning
				model := m.activeModel
				port := m.activePort
				m.mu.Unlock()
				m.send(ServerStartedMsg{
					Model:   model,
					Address: fmt.Sprintf("http://localhost:%d", port),
				})
			} else {
				m.mu.Unlock()
			}
		} else {
			// Health check failed — either the process exited or the 60-second
			// deadline elapsed. If the context was cancelled (explicit unload or
			// the wait goroutine already cleaned up), exit silently — no spurious
			// "failed to start" log entry in that case.
			if ctx.Err() != nil {
				return
			}

			// Surface a meaningful diagnostic through the log and ensure the
			// process is cleaned up so the wait goroutine delivers ServerStoppedMsg.
			m.logMu.Lock()
			var lastLog string
			if len(m.logBuf) > 0 {
				lastLog = m.logBuf[len(m.logBuf)-1]
			}
			m.logMu.Unlock()

			hint := "llama-server failed to start"
			if lastLog != "" {
				hint = fmt.Sprintf("llama-server failed to start: %s", lastLog)
			}
			m.appendLog("[llama-tui] " + hint)

			// Terminate the process so the wait goroutine delivers ServerStoppedMsg.
			m.mu.Lock()
			cmd := m.cmd
			cancel := m.cancel
			m.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			if cmd != nil && cmd.Process != nil {
				_ = cmd.Process.Signal(os.Interrupt)
			}
		}
	}()

	return nil
}

// UnloadModel gracefully stops the running server.
func (m *Manager) UnloadModel() error {
	m.mu.Lock()
	cmd := m.cmd
	cancel := m.cancel
	state := m.state
	waitDone := m.waitDone
	m.mu.Unlock()

	if cmd == nil || state == StateStopped {
		return nil
	}

	// Send SIGTERM.
	if cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
	}

	// Cancel the context so exec.CommandContext will also clean up.
	if cancel != nil {
		cancel()
	}

	// Wait for the scanner goroutine to drain and call Wait().
	// We never call cmd.Wait() here — only the scanner goroutine does.
	select {
	case <-waitDone:
		// Exited cleanly.
	case <-time.After(5 * time.Second):
		// Timed out — forcibly kill the process.
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-waitDone // Still wait for the goroutine to finish.
	}

	m.mu.Lock()
	m.cmd = nil
	m.cancel = nil
	m.waitDone = nil
	m.state = StateStopped
	m.activeModel = ""
	m.activePort = 0
	m.mu.Unlock()

	return nil
}

// Logs returns the recent log lines (up to 200).
func (m *Manager) Logs() []string {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	out := make([]string, len(m.logBuf))
	copy(out, m.logBuf)
	return out
}

// IsInstalled returns true if the llama-server binary file exists on disk.
func (m *Manager) IsInstalled() bool {
	path := m.cfg.LlamaServerPath()
	// Resolve symlinks / relative paths.
	if !filepath.IsAbs(path) {
		if abs, err := exec.LookPath(path); err == nil {
			path = abs
		}
	}
	_, err := os.Stat(path)
	return err == nil
}

// IsHealthy verifies that llama-server is installed AND can actually be
// executed. It runs `llama-server --version` with a short timeout.
// A binary that crashes immediately (e.g. due to missing .dylib dependencies)
// returns false so the caller can trigger a clean reinstall.
func (m *Manager) IsHealthy() bool {
	if !m.IsInstalled() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.cfg.LlamaServerPath(), "--version")
	err := cmd.Run()
	// llama-server --version exits 0 or 1 — either is fine.
	// What we're guarding against is a crash (exit 2+, signal).
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Non-zero exit from --version is acceptable (some versions exit 1).
			code := exitErr.ExitCode()
			if code == 1 || code == 0 {
				return true
			}
			// Signal kill / abort trap → not healthy.
			return false
		}
		// exec.Error (e.g. binary not found) or context timeout.
		return false
	}
	return true
}

// appendLog adds a line to the ring buffer, evicting the oldest when full.
func (m *Manager) appendLog(line string) {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	if len(m.logBuf) >= maxLogLines {
		// Drop oldest element.
		copy(m.logBuf, m.logBuf[1:])
		m.logBuf[len(m.logBuf)-1] = line
	} else {
		m.logBuf = append(m.logBuf, line)
	}
}

// send delivers a message to the registered TUI program (if any).
func (m *Manager) send(msg tea.Msg) {
	m.mu.Lock()
	p := m.program
	m.mu.Unlock()
	if p != nil {
		p.Send(msg)
	}
}

// waitForHealth polls GET http://127.0.0.1:<port>/health every 500ms for up to
// 60 seconds, returning true as soon as an HTTP 200 is received.
// Returns false if the context is cancelled or the deadline is exceeded.
func waitForHealth(ctx context.Context, port int) bool {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(60 * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(500 * time.Millisecond):
		}
	}
	return false
}
