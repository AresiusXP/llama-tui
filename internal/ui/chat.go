package ui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/patriciodanos/llama-tui/internal/llamaserver"
)

// CloseChatMsg is sent when user presses Esc to return to detail panel.
type CloseChatMsg struct{}

// ChatResponseMsg carries a completed assistant response.
type ChatResponseMsg struct {
	Content string
	Err     error
}

// ChatEntry is a single message in the conversation.
type ChatEntry struct {
	Role    string // "user" or "assistant"
	Content string
	IsError bool
}

// ChatModel is the Bubble Tea model for the inline chat panel.
type ChatModel struct {
	messages   []ChatEntry
	input      textinput.Model
	spinner    spinner.Model
	waiting    bool   // true while waiting for LLM response
	serverAddr string // e.g. "http://localhost:8080"
	modelName  string // display name
	width      int
	height     int
	scrollTop  int // for scrolling message history
}

// NewChat creates a new ChatModel.
func NewChat(serverAddr, modelName string, width, height int) ChatModel {
	ti := textinput.New()
	ti.Placeholder = "Type your message..."
	ti.Focus()
	ti.CharLimit = 2000

	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorYellow))

	return ChatModel{
		messages:   []ChatEntry{},
		input:      ti,
		spinner:    sp,
		waiting:    false,
		serverAddr: serverAddr,
		modelName:  modelName,
		width:      width,
		height:     height,
		scrollTop:  0,
	}
}

// SetSize updates the panel dimensions.
func (m *ChatModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetServerAddr updates the server address.
func (m *ChatModel) SetServerAddr(addr string) {
	m.serverAddr = addr
}

// Init starts the spinner tick command.
func (m ChatModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages and key events.
func (m ChatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc:
			return m, func() tea.Msg { return CloseChatMsg{} }

		case tea.KeyCtrlC:
			return m, tea.Quit

		case tea.KeyEnter:
			if m.waiting {
				return m, nil
			}
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, nil
			}
			// Append user message
			m.messages = append(m.messages, ChatEntry{Role: "user", Content: text})
			m.input.SetValue("")
			m.waiting = true
			// Auto-scroll to bottom
			m.scrollTop = 0

			// Build message history for API call
			apiMsgs := m.buildAPIMessages()
			cmds = append(cmds, sendChatCmd(m.serverAddr, apiMsgs), m.spinner.Tick)
			return m, tea.Batch(cmds...)

		case tea.KeyUp:
			if m.scrollTop < len(m.messages)-1 {
				m.scrollTop++
			}
			return m, nil

		case tea.KeyDown:
			if m.scrollTop > 0 {
				m.scrollTop--
			}
			return m, nil
		}

	case ChatResponseMsg:
		m.waiting = false
		if msg.Err != nil {
			m.messages = append(m.messages, ChatEntry{
				Role:    "assistant",
				Content: msg.Err.Error(),
				IsError: true,
			})
		} else {
			m.messages = append(m.messages, ChatEntry{
				Role:    "assistant",
				Content: msg.Content,
			})
		}
		m.scrollTop = 0
		return m, nil

	case spinner.TickMsg:
		if m.waiting {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}

	// Delegate to text input when not waiting
	if !m.waiting {
		var inputCmd tea.Cmd
		m.input, inputCmd = m.input.Update(msg)
		cmds = append(cmds, inputCmd)
	}

	return m, tea.Batch(cmds...)
}

// View renders the chat panel content (without a border — RenderFrame adds that).
func (m ChatModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	// No border here — RenderFrame provides the panel border.
	contentWidth := m.width
	if contentWidth < 1 {
		contentWidth = 1
	}

	// Header: "Chat · ModelName"
	titleLabel := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAccent)).
		Bold(true).
		Render("Chat")
	titleSep := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorTextDim)).
		Render("  ·  ")
	titleModel := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorText)).
		Render(m.modelName)
	header := titleLabel + titleSep + titleModel

	divider := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorBorder)).
		Render(strings.Repeat("─", contentWidth))

	// Input bar (fixed height: 1 line + padding)
	inputStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorTextDim)).
		Render("> ")
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorTextDim)).
		Render("  [Enter] Send  [Esc] Close")
	inputBar := inputStyle + m.input.View() + hint

	// Fixed lines used: header(1) + divider(1) + blank(1) + divider(1) + inputBar(1).
	// When waiting, the spinner takes an extra line below the messages area.
	fixedLines := 1 + 1 + 1 + 1 + 1
	if m.waiting {
		fixedLines++
	}
	messagesHeight := m.height - fixedLines
	if messagesHeight < 1 {
		messagesHeight = 1
	}

	// Render all message lines
	allLines := m.renderMessages(contentWidth)

	// Apply scroll: scrollTop=0 means show bottom, scrollTop>0 means scroll up
	totalLines := len(allLines)
	if totalLines > messagesHeight {
		// Default: show bottom
		end := totalLines - m.scrollTop
		if end < messagesHeight {
			end = messagesHeight
		}
		if end > totalLines {
			end = totalLines
		}
		start := end - messagesHeight
		if start < 0 {
			start = 0
		}
		allLines = allLines[start:end]
	}

	// Pad with blank lines if fewer lines than messagesHeight
	for len(allLines) < messagesHeight {
		allLines = append(allLines, "")
	}

	messagesArea := strings.Join(allLines, "\n")

	// Spinner line (shown only when waiting)
	spinnerLine := ""
	if m.waiting {
		spinnerLine = "\n" + lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorYellow)).
			Render(m.spinner.View()+" Waiting for response...")
	}

	body := strings.Join([]string{
		header,
		divider,
		"",
		messagesArea + spinnerLine,
		divider,
		inputBar,
	}, "\n")

	return body
}

// renderMessages converts ChatEntry slice into wrapped display lines.
func (m ChatModel) renderMessages(contentWidth int) []string {
	var lines []string
	labelWidth := 4 // "You:" or "AI: " = 4 chars + space

	wrapWidth := contentWidth - labelWidth - 1
	if wrapWidth < 10 {
		wrapWidth = 10
	}

	for _, entry := range m.messages {
		switch entry.Role {
		case "user":
			label := lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorCyan)).
				Bold(true).
				Render("You:")
			wrapped := wrapText(entry.Content, wrapWidth)
			for i, line := range wrapped {
				styledLine := lipgloss.NewStyle().
					Foreground(lipgloss.Color(ColorCyan)).
					Render(line)
				if i == 0 {
					lines = append(lines, label+" "+styledLine)
				} else {
					indent := strings.Repeat(" ", labelWidth+1)
					lines = append(lines, indent+styledLine)
				}
			}

		case "assistant":
			var label string
			if entry.IsError {
				label = lipgloss.NewStyle().
					Foreground(lipgloss.Color(ColorRed)).
					Bold(true).
					Render("AI:")
			} else {
				label = lipgloss.NewStyle().
					Foreground(lipgloss.Color(ColorAccent)).
					Bold(true).
					Render("AI:")
			}
			wrapped := wrapText(entry.Content, wrapWidth)
			for i, line := range wrapped {
				var styledLine string
				if entry.IsError {
					styledLine = StyleError.Render(line)
				} else {
					styledLine = lipgloss.NewStyle().
						Foreground(lipgloss.Color(ColorText)).
						Render(line)
				}
				if i == 0 {
					lines = append(lines, label+" "+styledLine)
				} else {
					indent := strings.Repeat(" ", labelWidth+1)
					lines = append(lines, indent+styledLine)
				}
			}
		}

		// Blank line between entries
		lines = append(lines, "")
	}

	return lines
}

// wrapText wraps a string to maxWidth characters per line.
func wrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}

	var result []string
	// Split on existing newlines first
	paragraphs := strings.Split(text, "\n")
	for _, para := range paragraphs {
		if len(para) == 0 {
			result = append(result, "")
			continue
		}
		words := strings.Fields(para)
		if len(words) == 0 {
			result = append(result, "")
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			if len(line)+1+len(word) <= maxWidth {
				line += " " + word
			} else {
				result = append(result, line)
				line = word
			}
		}
		result = append(result, line)
	}
	return result
}

// buildAPIMessages converts ChatEntry history to llamaserver.Message slice.
func (m ChatModel) buildAPIMessages() []llamaserver.Message {
	msgs := make([]llamaserver.Message, 0, len(m.messages))
	for _, e := range m.messages {
		if e.IsError {
			continue // skip error entries when building API payload
		}
		msgs = append(msgs, llamaserver.Message{
			Role:    e.Role,
			Content: e.Content,
		})
	}
	return msgs
}

// sendChatCmd returns a tea.Cmd that calls the LLM and returns a ChatResponseMsg.
func sendChatCmd(addr string, messages []llamaserver.Message) tea.Cmd {
	return func() tea.Msg {
		client := llamaserver.NewClient(addr)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		reply, err := client.Chat(ctx, messages)
		return ChatResponseMsg{Content: reply, Err: err}
	}
}
