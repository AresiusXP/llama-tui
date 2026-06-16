// Package app is the root Bubble Tea model for llama-tui.
// It holds global state and delegates to sub-models for each view.
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/patriciodanos/llama-tui/internal/config"
	"github.com/patriciodanos/llama-tui/internal/hardware"
	"github.com/patriciodanos/llama-tui/internal/huggingface"
	"github.com/patriciodanos/llama-tui/internal/llamaserver"
	"github.com/patriciodanos/llama-tui/internal/ui"
	"github.com/patriciodanos/llama-tui/internal/updater"
)

// activeView identifies which screen is active.
type activeView int

const (
	viewMain     activeView = iota
	viewSearch
	viewChat
	viewSettings
	viewHelp
)

// updateAvailable holds pending update notification.
type updateAvailable struct {
	appUpdate    updater.UpdateInfo
	llamaUpdate  updater.UpdateInfo
}

// Model is the root Bubble Tea model.
type Model struct {
	cfg     *config.Config
	version string

	// terminal dimensions
	width  int
	height int
	ready  bool

	// active view
	view      activeView
	leftFocus bool // true = left panel has focus

	// layout
	layout ui.Layout

	// sub-models
	help     ui.HelpModel
	library  ui.LibraryModel
	detail   ui.DetailModel
	search   ui.SearchModel
	chat     ui.ChatModel
	settings ui.SettingsModel
	status   ui.StatusPanelModel
	logs     ui.LogPanelModel

	// server manager
	manager *llamaserver.Manager

	// tea.Program reference — stored so goroutines can p.Send() async events
	program *tea.Program

	// downloadCancel cancels the current in-progress download (nil if none).
	downloadCancel context.CancelFunc

	// activeDownload tracks the current download's remote info for resume support.
	activeDownload struct {
		repoID         string
		remoteFilename string
		localFilename  string // base filename on disk
	}

	// confirmDelete — when non-nil, the delete confirmation modal is showing.
	confirmDelete *ui.ConfirmModel

	// detected GPUs
	gpus []hardware.GPU

	// active GPU index (mirrors cfg.Server.SelectedGPUIndex)
	selectedGPUIndex int

	// pending update info (populated by background check)
	updates updateAvailable

	// llama-server install/update state
	llamaInstalling bool    // true while llama-server is being downloaded
	llamaInstallPct float64 // 0.0–1.0 install progress

	// cached llama-server health (computed async — never call IsHealthy in View/Update)
	llamaHealthy     bool   // cached result of the last health check
	llamaChecked     bool   // whether at least one health check has completed
	llamaInstallTried bool  // guard: auto-install attempted this session (prevents loops)
	pendingLoadPath  string // model path to auto-load once llama-server is ready

	// server log ring buffer (last 5 lines shown in status)
	serverLogs []string

	// metricsPolling is true while the periodic metrics poller is running.
	// It prevents double-arming and stops re-arming after server stop.
	metricsPolling bool

	// metricsEpoch is incremented each time a model is started or stopped.
	// ServerMetricsMsg carries the epoch it was launched with; messages from
	// a prior epoch are silently discarded to prevent stale-data overwrites
	// and goroutine leaks from old polling loops.
	metricsEpoch int

	// notification message (transient — shown in action bar)
	notification string
}

// New creates the root application model.
func New(cfg *config.Config, version string) *Model {
	manager := llamaserver.NewManager(cfg)

	m := &Model{
		cfg:              cfg,
		version:          version,
		leftFocus:        true,
		view:             viewMain,
		manager:          manager,
		selectedGPUIndex: cfg.Server.SelectedGPUIndex,
	}

	return m
}

// SetProgram wires the tea.Program into the manager for async messages.
func (m *Model) SetProgram(p *tea.Program) {
	m.program = p
	m.manager.SetProgram(p)
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	// Health is checked asynchronously — never run the llama-server --version
	// subprocess on the event loop. The install-if-needed decision is made in
	// the llamaHealthMsg handler once the check returns.
	return tea.Batch(
		detectGPUsCmd(),
		checkUpdatesCmd(m.version, m.cfg.Update.LlamaCPPBuildTag),
		checkHealthCmd(m.manager),
	)
}

// ── Commands ─────────────────────────────────────────────────────────────────

func detectGPUsCmd() tea.Cmd {
	return func() tea.Msg {
		return gpusDetectedMsg{gpus: hardware.DetectGPUs()}
	}
}

// gpusDetectedMsg is the internal message for GPU detection results.
type gpusDetectedMsg struct {
	gpus []hardware.GPU
}

// llamaHealthMsg carries the result of an async health check.
type llamaHealthMsg struct{ healthy bool }

// checkHealthCmd runs the (potentially slow) llama-server --version check off
// the event loop, returning the result as a message. NEVER call IsHealthy()
// directly from Update or View — it spawns a subprocess and blocks the UI.
func checkHealthCmd(mgr *llamaserver.Manager) tea.Cmd {
	return func() tea.Msg {
		return llamaHealthMsg{healthy: mgr.IsHealthy()}
	}
}

// modelLoadFailedMsg reports a failed model load (errors only; success is
// delivered asynchronously via ServerStartedMsg from the manager).
type modelLoadFailedMsg struct{ err error }

// loadModelCmd runs Manager.LoadModel OFF the event loop. LoadModel internally
// calls UnloadModel which can block up to 5s waiting for a previous process to
// exit, so it must never run synchronously inside Update. Success is reported
// asynchronously by the manager via ServerStartedMsg/ServerStoppedMsg.
func loadModelCmd(mgr *llamaserver.Manager, runCfg llamaserver.ModelRunConfig) tea.Cmd {
	return func() tea.Msg {
		if err := mgr.LoadModel(runCfg); err != nil {
			return modelLoadFailedMsg{err: err}
		}
		return nil
	}
}

func checkUpdatesCmd(version, currentLlamaBuildTag string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		appInfo := updater.CheckAppUpdate(ctx, version)
		llamaInfo := updater.CheckLlamaServerUpdate(ctx, currentLlamaBuildTag)
		return updatesCheckedMsg{app: appInfo, llama: llamaInfo}
	}
}

// updatesCheckedMsg carries update check results.
type updatesCheckedMsg struct {
	app   updater.UpdateInfo
	llama updater.UpdateInfo
}

func refreshLibraryCmd(modelsDir string) tea.Cmd {
	return func() tea.Msg {
		return refreshLibraryMsg{modelsDir: modelsDir}
	}
}

// refreshLibraryMsg triggers a library refresh in the Update loop.
type refreshLibraryMsg struct {
	modelsDir string
}

// clearNotificationMsg is sent after a delay to clear the action bar notification.
type clearNotificationMsg struct{}

// llamaInstallProgressMsg carries download progress for llama-server install.
type llamaInstallProgressMsg struct{ pct float64 }

// llamaInstallDoneMsg signals that llama-server install finished.
type llamaInstallDoneMsg struct {
	err error
	tag string // the installed build tag, e.g. "b9667"
}

// installLlamaServerCmd downloads and installs llama-server.
// If info is non-nil it uses that already-fetched release info (for explicit
// update triggering); if nil it fetches the latest release first.
func (m *Model) installLlamaServerCmd(info *updater.UpdateInfo) tea.Cmd {
	return m.doInstallLlamaServer(info)
}

// installLlamaServerCmdFromInfo triggers install using a known UpdateInfo.
func (m *Model) installLlamaServerCmdFromInfo(info updater.UpdateInfo) tea.Cmd {
	return m.doInstallLlamaServer(&info)
}

// doInstallLlamaServer is the shared implementation for both install and update.
func (m *Model) doInstallLlamaServer(info *updater.UpdateInfo) tea.Cmd {
	return func() tea.Msg {
		p := m.program
		destPath := config.DefaultLlamaServerPath()

		go func() {
			ctx := context.Background()

			var release updater.Release
			if info != nil && info.LatestVersion != "" {
				// Re-fetch the full release object for the known tag.
				r, err := updater.GetRelease(ctx, "ggml-org", "llama.cpp", info.LatestVersion)
				if err != nil {
					if p != nil {
						p.Send(llamaInstallDoneMsg{err: fmt.Errorf("fetch release: %w", err)})
					}
					return
				}
				release = r
			} else {
				// First-run: fetch latest.
				r, err := updater.GetLatestLlamaRelease(ctx)
				if err != nil {
					if p != nil {
						p.Send(llamaInstallDoneMsg{err: fmt.Errorf("fetch latest release: %w", err)})
					}
					return
				}
				release = r
			}

			ch := updater.DownloadLlamaServer(ctx, release, destPath)
			for pct := range ch {
				if pct == -2.0 {
					if p != nil {
						p.Send(llamaInstallDoneMsg{err: fmt.Errorf("download failed")})
					}
					return
				}
				if pct == -1.0 {
					if p != nil {
						p.Send(llamaInstallDoneMsg{tag: release.TagName})
					}
					return
				}
				if p != nil {
					p.Send(llamaInstallProgressMsg{pct: pct})
				}
			}
		}()
		return nil
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

// pollMetricsInterval is how often the metrics poller fires while a model is running.
const pollMetricsInterval = 2 * time.Second

// pollMetricsCmd fires FetchMetrics off the event loop after a short delay and
// delivers the result as a ServerMetricsMsg tagged with the current epoch.
// The caller re-arms it on each receipt while metricsPolling is true and the
// epoch still matches, creating a self-renewing ticker that stops automatically
// when the model is unloaded (epoch changes) or metricsPolling is cleared.
func (m *Model) pollMetricsCmd(epoch int) tea.Cmd {
	addr := m.manager.ActiveAddress()
	metricsEnabled := m.cfg.Server.MetricsEnabled
	return tea.Tick(pollMetricsInterval, func(_ time.Time) tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		metrics := llamaserver.FetchMetrics(ctx, addr, metricsEnabled)
		return llamaserver.ServerMetricsMsg{Metrics: metrics, Epoch: epoch}
	})
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	// ── Window resize ────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout = ui.NewLayout(msg.Width, msg.Height, m.cfg.Server.MetricsEnabled)
		m.help = ui.NewHelp(msg.Width, msg.Height)
		firstResize := !m.ready
		if firstResize {
			// First resize: construct sub-models from scratch.
			m.library = ui.NewLibrary(m.cfg.ModelsDir, m.layout.LeftWidth-4, m.layout.LeftTopHeight)
			m.detail = ui.NewDetail(m.layout.RightWidth-4, m.layout.RightBottomHeight)
			m.status = ui.NewStatusPanelModel()
			m.status.LlamaVersion = m.cfg.Update.LlamaCPPBuildTag
			m.status.AppVersion = m.version
			m.status.GPU = m.activeGPUName()
			m.logs = ui.NewLogPanelModel()
		} else {
			// Subsequent resize: use SetSize to preserve model state
			// (cursor position, selected model, server status, active download, etc.)
			m.library.SetSize(m.layout.LeftWidth-4, m.layout.LeftTopHeight)
			m.detail.SetSize(m.layout.RightWidth-4, m.layout.RightBottomHeight)
			m.chat.SetSize(m.layout.RightWidth-2, m.layout.RightBottomHeight)
		}
		m.logs.SetSize(m.layout.LeftWidth-4, m.layout.LeftBottomHeight)
		// Restore focus state on the sub-models.
		m.library.SetFocus(m.leftFocus)
		m.detail.SetFocus(!m.leftFocus)
		if m.manager != nil {
			m.detail.SetServerState(
				m.manager.State().String(),
				m.manager.ActiveAddress(),
				m.activeGPUName(),
			)
		}
		m.ready = true
		if firstResize {
			return m, refreshLibraryCmd(m.cfg.ModelsDir)
		}

	// ── GPU detection ────────────────────────────────────────────────────
	case gpusDetectedMsg:
		m.gpus = msg.gpus
		if m.selectedGPUIndex >= len(m.gpus) {
			m.selectedGPUIndex = 0
		}
		m.detail.SetServerState(
			m.manager.State().String(),
			m.manager.ActiveAddress(),
			m.activeGPUName(),
		)
		m.status.GPU = m.activeGPUName()

	// ── Update check ─────────────────────────────────────────────────────
	case updatesCheckedMsg:
		m.updates = updateAvailable{appUpdate: msg.app, llamaUpdate: msg.llama}

	// ── llama-server health result (async) ────────────────────────────────
	case llamaHealthMsg:
		m.llamaHealthy = msg.healthy
		m.llamaChecked = true
		// Auto-install only ONCE per session to avoid an infinite
		// install→unhealthy→install loop when the binary is persistently broken.
		if !msg.healthy && !m.llamaInstalling && !m.llamaInstallTried {
			m.llamaInstallTried = true
			m.llamaInstalling = true
			return m, tea.Batch(
				m.installLlamaServerCmd(nil),
				m.notify("Installing llama-server…"),
			)
		}
		if !msg.healthy && m.llamaInstallTried && !m.llamaInstalling {
			// Already tried and still broken — tell the user instead of looping.
			return m, m.notify("llama-server install failed health check — press Ctrl+U to retry")
		}

	// ── llama-server install progress ─────────────────────────────────────
	case llamaInstallProgressMsg:
		m.llamaInstallPct = msg.pct

	// ── llama-server install done ─────────────────────────────────────────
	case llamaInstallDoneMsg:
		m.llamaInstalling = false
		m.llamaInstallPct = 0
		if msg.err != nil {
			m.pendingLoadPath = "" // give up the pending load on failure
			return m, m.notify("llama-server install failed: " + msg.err.Error())
		}
		// We just extracted a verified release — mark healthy without re-running
		// the subprocess synchronously. A background re-check confirms it.
		m.llamaHealthy = true
		m.llamaChecked = true
		m.cfg.Update.LlamaCPPBuildTag = msg.tag
		m.status.LlamaVersion = msg.tag
		_ = m.cfg.Save()

		cmds := []tea.Cmd{
			checkUpdatesCmd(m.version, msg.tag),
			checkHealthCmd(m.manager),
		}
		// Auto-load the model the user was trying to load before the install.
		if m.pendingLoadPath != "" {
			path := m.pendingLoadPath
			m.pendingLoadPath = ""
			runCfg := m.buildRunConfig(path)
			m.detail.SetServerState("STARTING", "", m.activeGPUName())
			// Async load — never block the event loop.
			cmds = append(cmds,
				loadModelCmd(m.manager, runCfg),
				m.notify("llama-server installed ("+msg.tag+") — loading model…"),
			)
		} else {
			cmds = append(cmds, m.notify("llama-server installed ("+msg.tag+")"))
		}
		return m, tea.Batch(cmds...)

	// ── Clear notification ────────────────────────────────────────────────
	case clearNotificationMsg:
		m.notification = ""

	// ── Library refresh ──────────────────────────────────────────────────
	case refreshLibraryMsg:
		if err := m.library.Refresh(); err != nil {
			return m, m.notify("Library refresh failed: " + err.Error())
		}
		if active := m.manager.ActiveModel(); active != "" {
			m.library.SetActiveModel(active)
		}
		// Sync detail panel with whatever model the library now has selected.
		m.detail.SetModel(m.library.SelectedModel())

	// ── llama-server events ──────────────────────────────────────────────
	case llamaserver.ServerStartedMsg:
		m.library.SetActiveModel(msg.Model)
		// Sync the detail panel's model snapshot so lm.Status reflects StatusLoaded
		// immediately. Without this, detail.View() shows "○ AVAILABLE" because it
		// holds a stale pre-load copy of the model.
		m.detail.SetModel(m.library.SelectedModel())
		m.detail.SetServerState("RUNNING", msg.Address, m.activeGPUName())
		m.logs.SetLogs(m.serverLogs)
		m.status.ModelLoaded = true
		m.status.UsageStats = "Active"
		m.status.GPU = m.activeGPUName()
		m.status.LlamaVersion = m.cfg.Update.LlamaCPPBuildTag
		m.status.AppVersion = m.version
		m.status.MetricsEnabled = m.cfg.Server.MetricsEnabled
		m.status.MetricsReady = false // first poll not yet returned
		// Recompute layout immediately so the status panel grows to 2 lines
		// when --metrics is enabled, without requiring a terminal resize.
		m.layout = ui.NewLayout(m.width, m.height, m.cfg.Server.MetricsEnabled)
		m.detail.SetSize(m.layout.RightWidth-4, m.layout.RightBottomHeight)
		m.chat.SetSize(m.layout.RightWidth-2, m.layout.RightBottomHeight)
		// Start the metrics poller if not already running.
		// Increment the epoch so any in-flight message from a prior session is ignored.
		m.metricsEpoch++
		cmds := []tea.Cmd{m.notify(fmt.Sprintf("Model loaded · %s", msg.Address))}
		if !m.metricsPolling {
			m.metricsPolling = true
			cmds = append(cmds, m.pollMetricsCmd(m.metricsEpoch))
		}
		return m, tea.Batch(cmds...)

	case llamaserver.ServerStoppedMsg:
		m.library.SetActiveModel("")
		m.detail.SetServerState("STOPPED", "", m.activeGPUName())
		// Push final log lines to the log panel.
		m.logs.SetLogs(m.serverLogs)
		// Stop the metrics poller and clear live metrics.
		// Increment epoch so any in-flight poll message is silently discarded.
		m.metricsEpoch++
		m.metricsPolling = false
		m.status.ModelLoaded = false
		m.status.UsageStats = "Idle"
		m.status.GPU = m.activeGPUName()
		m.status.LlamaVersion = m.cfg.Update.LlamaCPPBuildTag
		m.status.AppVersion = m.version
		m.status.MetricsEnabled = false
		m.status.MetricsReady = false
		m.status.GenerationTPS = 0
		m.status.PromptTPS = 0
		m.status.RequestsProcessing = 0
		m.status.ActiveSlots = 0
		m.status.TotalSlots = 0
		// Recompute layout so the status panel shrinks back to 1 line.
		m.layout = ui.NewLayout(m.width, m.height, false)
		m.detail.SetSize(m.layout.RightWidth-4, m.layout.RightBottomHeight)
		m.chat.SetSize(m.layout.RightWidth-2, m.layout.RightBottomHeight)
		var notifMsg string
		if msg.Err != nil {
			errStr := msg.Err.Error()
			notifMsg = "Server stopped: " + errStr
			m.logs.SetLastError(errStr)
		} else {
			notifMsg = "Model unloaded"
			m.logs.SetLastError("") // clear previous error on clean unload
		}
		if err := m.library.Refresh(); err != nil {
			notifMsg = "Library refresh failed: " + err.Error()
		}
		// Sync detail panel after library refresh.
		m.detail.SetModel(m.library.SelectedModel())
		return m, m.notify(notifMsg)

	case llamaserver.ServerLogMsg:
		m.serverLogs = appendLog(m.serverLogs, msg.Line, 100)
		m.logs.SetLogs(m.serverLogs)

	// ── Metrics poll result ───────────────────────────────────────────────
	case llamaserver.ServerMetricsMsg:
		// Discard messages from stale polling loops (previous model sessions).
		if !m.metricsPolling || msg.Epoch != m.metricsEpoch {
			return m, nil
		}
		m.status.GenerationTPS = msg.Metrics.GenerationTPS
		m.status.PromptTPS = msg.Metrics.PromptTPS
		m.status.RequestsProcessing = msg.Metrics.RequestsProcessing
		m.status.ActiveSlots = msg.Metrics.ActiveSlots
		m.status.TotalSlots = msg.Metrics.TotalSlots
		m.status.MetricsReady = true
		// Re-arm the poller for the next tick.
		return m, m.pollMetricsCmd(m.metricsEpoch)

	// ── Load model request ───────────────────────────────────────────────
	case ui.ModelLoadRequestMsg:
		// Guard: llama-server must be installed and healthy before loading.
		// Use the CACHED health flag — never call IsHealthy() here (it spawns
		// a subprocess and would block the event loop).
		if !m.llamaHealthy {
			if m.llamaInstalling {
				// Remember the load intent; it'll auto-run once install finishes.
				m.pendingLoadPath = msg.Model.Path
				return m, m.notify("llama-server is still installing — will load when ready…")
			}
			// Not checked yet, or known-unhealthy: trigger install + remember intent.
			m.pendingLoadPath = msg.Model.Path
			m.llamaInstalling = true
			return m, tea.Batch(
				m.installLlamaServerCmd(nil),
				m.notify("llama-server not ready — installing, will load when ready…"),
			)
		}
		runCfg := m.buildRunConfig(msg.Model.Path)
		m.detail.SetServerState("STARTING", "", m.activeGPUName())
		m.status.UsageStats = "Starting"
		// Run LoadModel off the event loop — it may block up to 5s unloading a
		// previous model. Success arrives via ServerStartedMsg.
		return m, tea.Batch(
			loadModelCmd(m.manager, runCfg),
			m.notify("Starting llama-server…"),
		)

	// ── Model load failed ─────────────────────────────────────────────────
	case modelLoadFailedMsg:
		m.detail.SetServerState("ERROR", "", m.activeGPUName())
		m.status.UsageStats = "Error"
		return m, m.notify("Failed to load: " + msg.err.Error())

	// ── Unload model request ─────────────────────────────────────────────
	case ui.ModelUnloadRequestMsg:
		m.status.UsageStats = "Stopping"
		go func() { _ = m.manager.UnloadModel() }()
		return m, m.notify("Unloading model…")

	// ── Model selection ──────────────────────────────────────────────────
	case ui.ModelSelectedMsg:
		lm := msg.Model
		m.detail.SetModel(&lm)

	// ── Delete model request → show confirmation modal ───────────────────
	case ui.ModelDeleteRequestMsg:
		cm := ui.NewConfirm(msg.Model, m.width, m.height)
		m.confirmDelete = &cm

	// ── Confirm delete: yes ───────────────────────────────────────────────
	case ui.ConfirmDeleteYesMsg:
		m.confirmDelete = nil
		var notifMsg string
		if err := os.Remove(msg.Model.Path); err != nil {
			notifMsg = "Delete failed: " + err.Error()
		} else {
			notifMsg = "Deleted " + msg.Model.Name
			m.detail.SetModel(nil)
		}
		if err := m.library.Refresh(); err != nil {
			notifMsg = "Library refresh failed: " + err.Error()
		}
		// Sync detail with the model now selected after deletion.
		// Only update if we just deleted the viewed model (detail was cleared above).
		m.detail.SetModel(m.library.SelectedModel())
		return m, m.notify(notifMsg)

	// ── Confirm delete: no / cancel ───────────────────────────────────────
	case ui.ConfirmDeleteNoMsg:
		m.confirmDelete = nil

	// ── Open search ──────────────────────────────────────────────────────
	case ui.OpenSearchMsg:
		m.search = ui.NewSearch(m.cfg.HuggingFace.Token, m.width, m.height)
		m.view = viewSearch
		return m, m.search.Init()

	// ── Close search ─────────────────────────────────────────────────────
	case ui.CloseSearchMsg:
		m.view = viewMain

	// ── Cancel download ───────────────────────────────────────────────────
	case ui.CancelDownloadMsg:
		if m.downloadCancel != nil {
			m.downloadCancel()
			m.downloadCancel = nil
		}
		// Mark the model as paused in-place (no Refresh — that would lose the status).
		m.library.MarkPaused(
			m.activeDownload.localFilename,
			m.activeDownload.repoID,
			m.activeDownload.remoteFilename,
		)
		return m, m.notify("Download paused · press [x] to resume")

	// ── Resume download ───────────────────────────────────────────────────
	case ui.ResumeDownloadMsg:
		return m, m.doDownload(msg.RepoID, msg.RemoteFilename, m.cfg.ModelsDir, m.cfg.HuggingFace.Token)

	// ── Download request (from search) ───────────────────────────────────
	case ui.DownloadRequestMsg:
		m.view = viewMain
		return m, m.doDownload(msg.RepoID, msg.Filename, m.cfg.ModelsDir, m.cfg.HuggingFace.Token)

	// ── Download progress ─────────────────────────────────────────────────
	case ui.DownloadProgressMsg:
		filename := filepath.Base(msg.Progress.Filename)
		pct := msg.Progress.Percent() / 100.0
		bytesDone := msg.Progress.BytesDone
		total := msg.Progress.BytesTotal
		if !m.library.UpdateDownloadProgress(filename, pct, bytesDone, total, msg.Progress.Done) {
			if err := m.library.Refresh(); err == nil {
				m.library.UpdateDownloadProgress(filename, pct, bytesDone, total, msg.Progress.Done)
			}
		}
		// Keep detail panel in sync with real-time download progress.
		m.detail.SetModel(m.library.SelectedModel())
		if msg.Progress.Done {
			if err := m.library.Refresh(); err != nil {
				return m, m.notify("Library refresh failed: " + err.Error())
			}
			m.detail.SetModel(m.library.SelectedModel())
		}

	// ── Download complete ─────────────────────────────────────────────────
	case ui.DownloadCompleteMsg:
		notifMsg := "Downloaded " + filepath.Base(msg.Filename)
		if err := m.library.Refresh(); err != nil {
			notifMsg = "Library refresh failed: " + err.Error()
		}
		m.detail.SetModel(m.library.SelectedModel())
		return m, m.notify(notifMsg)

	// ── Download error ────────────────────────────────────────────────────
	case ui.DownloadErrorMsg:
		return m, m.notify("Download failed: " + msg.Err.Error())

	// ── Open chat ─────────────────────────────────────────────────────────
	case ui.OpenChatMsg:
		addr := m.manager.ActiveAddress()
		if addr == "" {
			return m, m.notify("No model loaded — press [l] to load a model first")
		}
		modelName := ""
		if sel := m.library.SelectedModel(); sel != nil {
			modelName = sel.Name
		}
		m.chat = ui.NewChat(addr, modelName, m.layout.RightWidth-2, m.layout.RightBottomHeight)
		m.view = viewChat
		return m, m.chat.Init()

	// ── Close chat ────────────────────────────────────────────────────────
	case ui.CloseChatMsg:
		m.view = viewMain

	// ── Settings ──────────────────────────────────────────────────────────
	case ui.CloseSettingsMsg:
		if msg.Saved {
			if newCfg, err := config.Load(); err == nil {
				m.cfg = newCfg
				m.selectedGPUIndex = m.cfg.Server.SelectedGPUIndex
				m.library = ui.NewLibrary(m.cfg.ModelsDir, m.layout.LeftWidth-4, m.layout.LeftTopHeight)
				m.library.SetFocus(m.leftFocus) // restore focus on the newly-constructed model
				// Restore active model badge if a model is currently loaded.
				if active := m.manager.ActiveModel(); active != "" {
					m.library.SetActiveModel(active)
				}
				var notifMsg string
				if err := m.library.Refresh(); err != nil {
					notifMsg = "Library refresh failed: " + err.Error()
				} else {
					notifMsg = "Settings saved"
				}
				// Sync detail panel with the current library selection after recreate.
				m.detail.SetModel(m.library.SelectedModel())
				return m, m.notify(notifMsg)
			}
		}
		m.view = viewMain

	// ── Global key events ─────────────────────────────────────────────────
	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Delegate to active sub-model if applicable.
	return m.delegateUpdate(msg)
}

// handleKey processes global key events.
func (m *Model) handleKey(msg tea.KeyMsg) (*Model, tea.Cmd) {
	// Confirmation modal captures all keys when active.
	if m.confirmDelete != nil {
		nm, cmd := m.confirmDelete.Update(msg)
		if cm, ok := nm.(ui.ConfirmModel); ok {
			m.confirmDelete = &cm
		}
		return m, cmd
	}

	// Overlays capture all keys.
	switch m.view {
	case viewSearch:
		nm, cmd := m.search.Update(msg)
		if sm, ok := nm.(ui.SearchModel); ok {
			m.search = sm
		}
		return m, cmd
	case viewChat:
		nm, cmd := m.chat.Update(msg)
		if cm, ok := nm.(ui.ChatModel); ok {
			m.chat = cm
		}
		return m, cmd
	case viewSettings:
		nm, cmd := m.settings.Update(msg)
		if sm, ok := nm.(ui.SettingsModel); ok {
			m.settings = sm
		}
		return m, cmd
	case viewHelp:
		m.view = viewMain
		return m, nil
	}

	// Global keys.
	switch msg.String() {
	case "ctrl+c", "q":
		_ = m.manager.UnloadModel()
		return m, tea.Quit
	case "?":
		m.view = viewHelp
	case "tab":
		m.leftFocus = !m.leftFocus
		m.library.SetFocus(m.leftFocus)
		m.detail.SetFocus(!m.leftFocus)
	case "s", "S":
		m.settings = ui.NewSettings(m.cfg, m.gpus, m.width, m.height)
		m.view = viewSettings
		return m, m.settings.Init()
	case "d", "D":
		m.search = ui.NewSearch(m.cfg.HuggingFace.Token, m.width, m.height)
		m.view = viewSearch
		return m, m.search.Init()
	case "ctrl+u":
		// Install or update llama-server.
		if m.llamaInstalling {
			return m, m.notify("llama-server is already being installed, please wait…")
		}
		// Not installed/healthy yet → fresh install (manual retry resets the guard).
		if m.llamaChecked && !m.llamaHealthy {
			m.llamaInstallTried = true
			m.llamaInstalling = true
			return m, tea.Batch(
				m.installLlamaServerCmd(nil),
				m.notify("Installing llama-server…"),
			)
		}
		// Installed but a newer version is available → update.
		if m.updates.llamaUpdate.Available {
			m.llamaInstalling = true
			info := m.updates.llamaUpdate
			return m, tea.Batch(
				m.installLlamaServerCmdFromInfo(info),
				m.notify("Updating llama-server to "+info.LatestVersion+"…"),
			)
		}
		return m, m.notify("llama-server is already up to date")
	default:
		// Delegate to focused panel.
		if m.leftFocus {
			nm, cmd := m.library.Update(msg)
			if lm, ok := nm.(ui.LibraryModel); ok {
				m.library = lm
			}
			return m, cmd
		}
		nm, cmd := m.detail.Update(msg)
		if dm, ok := nm.(ui.DetailModel); ok {
			m.detail = dm
		}
		return m, cmd
	}

	return m, nil
}

// delegateUpdate passes non-key messages to sub-models.
func (m *Model) delegateUpdate(msg tea.Msg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch m.view {
	case viewSearch:
		nm, cmd := m.search.Update(msg)
		if sm, ok := nm.(ui.SearchModel); ok {
			m.search = sm
		}
		cmds = append(cmds, cmd)
	case viewChat:
		nm, cmd := m.chat.Update(msg)
		if cm, ok := nm.(ui.ChatModel); ok {
			m.chat = cm
		}
		cmds = append(cmds, cmd)
	case viewSettings:
		nm, cmd := m.settings.Update(msg)
		if sm, ok := nm.(ui.SettingsModel); ok {
			m.settings = sm
		}
		cmds = append(cmds, cmd)
	default:
		// Pass to both panels (for spinner ticks etc.)
		nm, cmd := m.library.Update(msg)
		if lm, ok := nm.(ui.LibraryModel); ok {
			m.library = lm
		}
		cmds = append(cmds, cmd)
		nm2, cmd2 := m.detail.Update(msg)
		if dm, ok := nm2.(ui.DetailModel); ok {
			m.detail = dm
		}
		cmds = append(cmds, cmd2)
	}

	return m, tea.Batch(cmds...)
}

// ── View ──────────────────────────────────────────────────────────────────────

// View implements tea.Model.
func (m *Model) View() string {
	if !m.ready {
		return "Loading llama-tui…\n"
	}

	// Confirmation modal overlays everything.
	if m.confirmDelete != nil {
		return m.confirmDelete.View()
	}

	switch m.view {
	case viewHelp:
		return m.help.View()
	case viewSearch:
		return m.search.View()
	case viewSettings:
		return m.settings.View()
	case viewChat:
		return ui.RenderFrame(m.layout,
			m.library.View(), m.status.View(), m.logs.View(), m.chat.View(),
			m.actionBar(), m.statusBar(), false)
	}

	// Main view: library left, detail right.
	return ui.RenderFrame(m.layout,
		m.library.View(), m.status.View(), m.logs.View(), m.detail.View(),
		m.actionBar(), m.statusBar(), m.leftFocus)
}

// statusBar builds the one-line status bar.
func (m *Model) statusBar() string {
	// Server state.
	var stateBadge string
	state := m.manager.State()
	switch state {
	case llamaserver.StateRunning:
		stateBadge = ui.StyleBadgeRunning.Render("● RUNNING")
	case llamaserver.StateStarting:
		stateBadge = ui.StyleBadgeDownload.Render("◌ STARTING")
	case llamaserver.StateError:
		stateBadge = ui.StyleBadgeStopped.Render("✕ ERROR")
	default:
		stateBadge = ui.StyleBadgeStopped.Render("● STOPPED")
	}

	addr := m.manager.ActiveAddress()
	if addr == "" {
		addr = fmt.Sprintf("http://localhost:%d", m.cfg.Server.Port)
	}
	addrStr := ui.StyleDim.Render(addr)

	// GPU info.
	gpuName := m.activeGPUName()
	if gpuName == "" {
		gpuName = "CPU"
	}
	gpuStr := ui.StyleDim.Render(gpuName)

	// llama.cpp version.
	// llama.cpp version — show install progress if in progress.
	llamaTag := m.cfg.Update.LlamaCPPBuildTag
	var llamaStr string
	if m.llamaInstalling {
		pctInt := int(m.llamaInstallPct * 100)
		llamaStr = ui.StyleBadgeDownload.Render(fmt.Sprintf("⟳ installing llama-server %d%%", pctInt))
	} else if llamaTag == "" {
		llamaStr = ui.StyleDim.Render("llama-server not installed")
	} else {
		llamaStr = ui.StyleDim.Render("llama.cpp " + llamaTag)
	}

	// App version.
	appStr := ui.StyleDim.Render("llama-tui " + m.version)

	// Update badges — only shown when not currently installing.
	var updateStr string
	if !m.llamaInstalling {
		if m.updates.appUpdate.Available {
			updateStr = " " + ui.StyleBadgeDownload.Render("[↑ app]")
		}
		if m.updates.llamaUpdate.Available {
			updateStr += " " + ui.StyleBadgeDownload.Render("[↑ llama.cpp]")
		}
	}

	// Status bar always shows server state — notifications go in the action bar.
	sep := ui.StyleDim.Render("  │  ")
	rightPart := llamaStr + "  │  " + gpuStr + "  │  " + appStr + updateStr
	bar := stateBadge + "  " + addrStr + sep + rightPart

	return lipgloss.NewStyle().Width(m.width).Render(bar)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// actionBar builds the one-line k9s-style context-sensitive action hints bar.
// Transient notifications appear here instead of the status bar.
func (m *Model) actionBar() string {
	type action struct {
		key  string
		desc string
	}

	// Determine actions based on the selected model's status.
	var actions []action

	sel := m.library.SelectedModel()
	if sel != nil {
		switch sel.Status {
		case ui.StatusDownloading:
			actions = []action{
				{"x", "Cancel download"},
				{"Ctrl+D", "Delete"},
			}
		case ui.StatusPaused:
			actions = []action{
				{"x", "Resume download"},
				{"Ctrl+D", "Delete"},
			}
		case ui.StatusLoaded:
			actions = []action{
				{"u", "Unload"},
				{"c", "Chat"},
				{"d", "Download more"},
				{"Ctrl+D", "Delete"},
			}
		default: // StatusAvailable
			actions = []action{
				{"l", "Load"},
				{"d", "Download more"},
				{"Ctrl+D", "Delete"},
			}
		}
	} else {
		actions = []action{
			{"d", "Download a model"},
		}
	}

	// Always-visible global actions.
	globals := []action{
		{"s", "Settings"},
		{"?", "Help"},
		{"q", "Quit"},
	}
	// Add update action when available and not already installing.
	if m.updates.llamaUpdate.Available && !m.llamaInstalling {
		globals = append([]action{{"Ctrl+U", "Update llama-server"}}, globals...)
	}
	if m.llamaInstalling {
		globals = append([]action{{"⟳", fmt.Sprintf("Installing llama-server %.0f%%", m.llamaInstallPct*100)}}, globals...)
	}
	// Use the CACHED health flag — never call IsHealthy() in View().
	if m.llamaChecked && !m.llamaHealthy && !m.llamaInstalling {
		globals = append([]action{{"Ctrl+U", "Install llama-server"}}, globals...)
	}
	actions = append(actions, globals...)

	// Render each action as "<key> desc".
	var parts []string
	for _, a := range actions {
		key := ui.StyleKey.Render("<" + a.key + ">")
		desc := ui.StyleDim.Render(a.desc)
		parts = append(parts, key+" "+desc)
	}
	actionsStr := strings.Join(parts, "  ")

	// Prepend notification if set (auto-clears after 3s via notify()).
	if m.notification != "" {
		notifStr := ui.StyleSuccess.Render("● " + m.notification)
		sep := ui.StyleDim.Render("  │  ")
		return notifStr + sep + actionsStr
	}

	return actionsStr
}

// notify sets a transient notification that appears in the action bar.
// It returns a tea.Cmd that clears the notification after 3 seconds.
func (m *Model) notify(msg string) tea.Cmd {
	m.notification = msg
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return clearNotificationMsg{}
	})
}

// activeGPUName returns the display name of the currently selected GPU.
func (m *Model) activeGPUName() string {
	if len(m.gpus) == 0 {
		return ""
	}
	if m.selectedGPUIndex >= len(m.gpus) {
		return m.gpus[0].Name
	}
	return m.gpus[m.selectedGPUIndex].Name
}

// buildRunConfig creates a ModelRunConfig from current settings.
func (m *Model) buildRunConfig(modelPath string) llamaserver.ModelRunConfig {
	var gpu *hardware.GPU
	if len(m.gpus) > 0 {
		idx := m.selectedGPUIndex
		if idx >= len(m.gpus) {
			idx = 0
		}
		g := m.gpus[idx]
		gpu = &g
	}

	return llamaserver.ModelRunConfig{
		ModelPath:      modelPath,
		Port:           m.cfg.Server.Port,
		ContextSize:    m.cfg.Server.ContextSize,
		GPULayers:      m.cfg.Server.GPULayers,
		GPU:            gpu,
		ParallelSlots:  m.cfg.Server.ParallelSlots,
		KVCacheTypeK:   m.cfg.Server.KVCacheTypeK,
		KVCacheTypeV:   m.cfg.Server.KVCacheTypeV,
		MetricsEnabled: m.cfg.Server.MetricsEnabled,
	}
}

// doDownload launches a background goroutine that streams download progress via
// p.Send, which is the correct Bubble Tea pattern for long-running async work.
// The returned tea.Cmd returns nil immediately (it just starts the goroutine).
// A cancellable context is stored in m.downloadCancel so the download can be
// stopped via CancelDownloadMsg.
func (m *Model) doDownload(repoID, filename, destDir, token string) tea.Cmd {
	return func() tea.Msg {
		// Cancel any previous in-flight download.
		if m.downloadCancel != nil {
			m.downloadCancel()
		}
		ctx, cancel := context.WithCancel(context.Background())
		m.downloadCancel = cancel

		// Record resume info so cancel can mark the model as paused.
		m.activeDownload.repoID = repoID
		m.activeDownload.remoteFilename = filename
		m.activeDownload.localFilename = filepath.Base(filename)

		p := m.program
		go func() {
			ch := huggingface.DownloadFile(ctx, repoID, filename, destDir, token)
			for prog := range ch {
				if prog.Err != nil {
					if p != nil {
						p.Send(ui.DownloadErrorMsg{Err: prog.Err})
					}
					return
				}
				if prog.Done {
					destPath := filepath.Join(destDir, filepath.Base(filename))
					if p != nil {
						p.Send(ui.DownloadCompleteMsg{
							RepoID:   repoID,
							Filename: filename,
							Path:     destPath,
						})
					}
					return
				}
				if p != nil {
					p.Send(ui.DownloadProgressMsg{Progress: prog})
				}
			}
		}()
		return nil
	}
}

// appendLog adds a line to a capped ring slice.
func appendLog(buf []string, line string, cap int) []string {
	buf = append(buf, line)
	if len(buf) > cap {
		buf = buf[len(buf)-cap:]
	}
	return buf
}
