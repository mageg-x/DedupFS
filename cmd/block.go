package cmd

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mageg-x/dedupfs/dfs"
	"github.com/mageg-x/dedupfs/internal/utils"
)

// focus enum for view focus management

const (
	focusBlockChunk int = iota
	focusBlockHex
)

// blockModel represents the UI model for block debugging
type blockModel struct {
	infoRows   [][]string
	headerRows [][]string
	chunkLines []string
	ChunkList  []*dfs.BlockChunk
	block      *dfs.Block
	mountPoint string

	selectedChunkIndex int
	hexContent         string
	hexLines           []string

	viewport viewport.Model
	hexVP    viewport.Model

	focus  int
	ready  bool
	width  int
	height int
}

func (m *blockModel) loadChunkHex() {
	if m.ChunkList == nil || len(m.ChunkList) == 0 {
		m.hexContent = "No valid chunks available."
		m.hexLines = strings.Split(m.hexContent, "\n")
		m.hexVP.SetContent(m.hexContent)
		return
	}

	if m.selectedChunkIndex < 0 || m.selectedChunkIndex >= len(m.ChunkList) {
		m.hexContent = "Select a chunk to view its hex content."
		m.hexLines = strings.Split(m.hexContent, "\n")
		m.hexVP.SetContent(m.hexContent)
		return
	}

	start := int32(0)
	for i := 0; i < m.selectedChunkIndex; i++ {
		start += m.ChunkList[i].Size
	}

	size := m.ChunkList[m.selectedChunkIndex].Size
	if start+size > int32(len(m.block.Data)) {
		m.hexContent = "Invalid chunk size or data mismatch."
		m.hexLines = strings.Split(m.hexContent, "\n")
		m.hexVP.SetContent(m.hexContent)
		return
	}

	data := m.block.Data[start : start+size]
	m.hexContent = hex.Dump(data)
	m.hexLines = strings.Split(m.hexContent, "\n")
	m.hexVP.SetContent(m.hexContent)
}

// scrollToLine scrolls the viewport to ensure the given lineIndex is visible.
// Compatible with older versions of bubbles/viewport that lack GotoLine.
func (m *blockModel) scrollToLine(vp *viewport.Model, lineIndex, totalLines int) {
	if totalLines == 0 || vp.Height <= 0 {
		return
	}

	if lineIndex < 0 {
		lineIndex = 0
	} else if lineIndex >= totalLines {
		lineIndex = totalLines - 1
	}

	// If already visible, do nothing
	if lineIndex >= vp.YOffset && lineIndex < vp.YOffset+vp.Height {
		return
	}

	// Simple strategy: put the line at the top
	vp.YOffset = lineIndex

	// Clamp to valid range
	maxOffset := totalLines - vp.Height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if vp.YOffset > maxOffset {
		vp.YOffset = maxOffset
	}
	if vp.YOffset < 0 {
		vp.YOffset = 0
	}
}

func (m *blockModel) scrollToSelectedChunk() {
	m.scrollToLine(&m.viewport, m.selectedChunkIndex, len(m.chunkLines))
}

func (m blockModel) Init() tea.Cmd {
	return nil
}

func (m blockModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case msg.Type == tea.KeyCtrlC, msg.String() == "q", msg.String() == "esc":
			return m, tea.Quit

		case msg.String() == "tab":
			if m.focus == focusBlockChunk {
				m.focus = focusBlockHex
			} else {
				m.focus = focusBlockChunk
			}

		case m.focus == focusBlockChunk:
			switch msg.String() {
			case "up":
				if len(m.chunkLines) > 0 && m.selectedChunkIndex > 0 {
					m.selectedChunkIndex--
					m.loadChunkHex()
					m.scrollToSelectedChunk()
				}
			case "down":
				if len(m.chunkLines) > 0 && m.selectedChunkIndex < len(m.chunkLines)-1 {
					m.selectedChunkIndex++
					m.loadChunkHex()
					m.scrollToSelectedChunk()
				}
			}

		case m.focus == focusBlockHex:
			var cmd tea.Cmd
			m.hexVP, cmd = m.hexVP.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}
	}

	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
		m.height = msg.Height
		if !m.ready {
			m.ready = true
		}
	}

	// Update both viewports for non-key or hex-focus events
	if _, ok := msg.(tea.KeyMsg); !ok {
		var cmd1, cmd2 tea.Cmd
		m.viewport, cmd1 = m.viewport.Update(msg)
		m.hexVP, cmd2 = m.hexVP.Update(msg)
		cmds = append(cmds, cmd1, cmd2)
	} else if m.focus == focusBlockHex {
		var cmd tea.Cmd
		m.hexVP, cmd = m.hexVP.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m blockModel) highlightLine(lines []string, lineIndex int, width int) string {
	if lineIndex < 0 || lineIndex >= len(lines) {
		return strings.Join(lines, "\n")
	}

	highlightStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#ffffff"}).
		Background(lipgloss.AdaptiveColor{Light: "#5D5FEF", Dark: "#4A4AEE"})

	newLines := make([]string, len(lines))
	for i, line := range lines {
		if i == lineIndex {
			if len(line) > width {
				line = line[:width]
			}
			newLines[i] = highlightStyle.Render(line)
		} else {
			newLines[i] = line
		}
	}
	return strings.Join(newLines, "\n")
}

func (m blockModel) View() string {
	if !m.ready {
		return "Loading block data..."
	}

	topBoxHeight := 14
	chunkSectionTitleHeight := 2
	helpHeight := 2
	availableHeight := m.height - topBoxHeight - chunkSectionTitleHeight - helpHeight
	if availableHeight < 6 {
		availableHeight = 6
	}
	innerHeight := availableHeight - 4
	if innerHeight < 3 {
		innerHeight = 3
	}

	halfWidth := (m.width - 4) / 2
	m.viewport.Width = halfWidth - 2
	m.viewport.Height = innerHeight
	m.hexVP.Width = halfWidth - 2
	m.hexVP.Height = innerHeight

	infoBoxColor := lipgloss.AdaptiveColor{Light: "#7D56E0", Dark: "#B39DDB"}
	headerBoxColor := lipgloss.AdaptiveColor{Light: "#E0569E", Dark: "#F48FB1"}
	chunkBoxColor := lipgloss.AdaptiveColor{Light: "#56E0A6", Dark: "#80CBC4"}
	hexBoxColor := lipgloss.AdaptiveColor{Light: "#E0A656", Dark: "#FFCC80"}
	helpColor := lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}

	infoBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(infoBoxColor).
		Padding(1, 2).
		Width(m.width/2 - 2).
		Height(14)

	headerBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(headerBoxColor).
		Padding(1, 2).
		Width(m.width/2 - 2).
		Height(14)

	renderRows := func(rows [][]string, maxWidth, maxLines int) string {
		var lines []string
		for i, r := range rows {
			if i >= maxLines {
				lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("... (truncated)"))
				break
			}
			if len(r) != 2 {
				continue
			}
			key := lipgloss.NewStyle().Width(14).Render(r[0] + ":")
			val := lipgloss.NewStyle().MaxWidth(maxWidth - 16).Render(r[1])
			lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, key, val))
		}
		return strings.Join(lines, "\n")
	}

	infoTitle := lipgloss.NewStyle().Foreground(infoBoxColor).Bold(true).Render("Block Info")
	headerTitle := lipgloss.NewStyle().Foreground(headerBoxColor).Bold(true).Render("Block Header")

	leftContent := infoTitle + "\n" + renderRows(m.infoRows, m.width/2-4, 20)
	rightContent := headerTitle + "\n" + renderRows(m.headerRows, m.width/2-4, 20)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top,
		infoBoxStyle.Render(leftContent),
		headerBoxStyle.Render(rightContent))

	// --- Chunk List: scrollable + auto-scroll to selection + no offset ---
	var displayedChunkLines []string
	highlightStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#ffffff"}).
		Background(lipgloss.AdaptiveColor{Light: "#5D5FEF", Dark: "#4A4AEE"})
		// No padding → keeps alignment

	for i, line := range m.chunkLines {
		if i == m.selectedChunkIndex {
			displayedChunkLines = append(displayedChunkLines, highlightStyle.Render(line))
		} else {
			displayedChunkLines = append(displayedChunkLines, line)
		}
	}
	m.viewport.SetContent(strings.Join(displayedChunkLines, "\n"))

	// --- Hex View: highlight first visible line ---
	hexContentWithHighlight := m.hexContent
	if len(m.hexLines) > 0 {
		currentLine := m.hexVP.YOffset
		hexContentWithHighlight = m.highlightLine(m.hexLines, currentLine, m.hexVP.Width)
	}
	m.hexVP.SetContent(hexContentWithHighlight)

	// Borders
	chunkBorderStyle := lipgloss.NormalBorder()
	hexBordertyle := lipgloss.NormalBorder()

	if m.focus == focusBlockChunk {
		chunkBorderStyle = lipgloss.ThickBorder()
	} else {
		hexBordertyle = lipgloss.ThickBorder()
	}

	chunkBoxStyle := lipgloss.NewStyle().
		Border(chunkBorderStyle).
		BorderForeground(chunkBoxColor).
		Padding(0, 1)

	hexBoxStyle := lipgloss.NewStyle().
		Border(hexBordertyle).
		BorderForeground(hexBoxColor).
		Padding(0, 1)

	chunkTitle := lipgloss.NewStyle().Foreground(chunkBoxColor).Bold(true).
		Render(fmt.Sprintf("Chunk List (%d items)", len(m.chunkLines)))
	hexTitle := lipgloss.NewStyle().Foreground(hexBoxColor).Bold(true).
		Render("Hex View")

	chunkBoxContent := chunkTitle + "\n" + m.viewport.View()
	hexBoxContent := hexTitle + "\n" + m.hexVP.View()

	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top,
		chunkBoxStyle.Width(halfWidth).Render(chunkBoxContent),
		hexBoxStyle.Width(halfWidth).Render(hexBoxContent))

	help := lipgloss.NewStyle().Foreground(helpColor).Render(
		"↑/↓: navigate • Tab: switch focus • q: quit",
	)

	finalView := topRow + "\n\n" + bottomRow + "\n\n" + help

	lines := strings.Split(finalView, "\n")
	if len(lines) > m.height {
		finalView = strings.Join(lines[:m.height], "\n")
	}

	return finalView
}

// debugBlockAction handles the block debugging functionality
func debugBlockAction(mountPoint, blockID string) error {
	// 读取block config 配置
	// user.dedupfs.blockconf="{\"Size\":67108864,\"Compress\":false,\"Encrypt\":false,\"Password\":\"\"}"
	blockConfBytes, err := utils.GetXAttr(mountPoint, "user.dedupfs.blockconf")
	if err != nil {
		return fmt.Errorf("failed to get user.dedupfs.blockconf: %w", err)
	}
	blockConf := &dfs.BlockConfig{}
	err = json.Unmarshal(blockConfBytes, blockConf)
	if err != nil {
		return fmt.Errorf("failed to unmarshal block config: %w", err)
	}

	dataDirBytes, err := utils.GetXAttr(mountPoint, "user.dedupfs.datadir")
	if err != nil {
		return fmt.Errorf("failed to get user.dedupfs.datadir: %w", err)
	}
	dataDir := string(dataDirBytes)

	blockPath := filepath.Join(dataDir, dfs.GetBlockPath(blockID))
	if _, err := os.Stat(blockPath); os.IsNotExist(err) {
		return fmt.Errorf("block not found: %s", blockID)
	} else if err != nil {
		return fmt.Errorf("failed to check block: %w", err)
	}

	blockData, err := ioutil.ReadFile(blockPath)
	if err != nil {
		return fmt.Errorf("failed to read block: %w", err)
	}

	infoRows := [][]string{
		{"Block ID", blockID},
		{"Mount Point", mountPoint},
		{"Data Directory", dataDir},
		{"Block Path", blockPath},
		{"Block Size", formatSize(int64(len(blockData))) + " bytes"},
	}

	var headerRows [][]string
	var chunkLines []string
	var ChunkList []*dfs.BlockChunk
	var block *dfs.Block

	block, err = dfs.DeserializeBlock(blockData)
	if err != nil {
		headerRows = [][]string{{"Parse Error", err.Error()}}
		chunkLines = []string{"Block structure is invalid or corrupted."}
		ChunkList = nil
		block = nil
	} else {
		headerRows = [][]string{
			{"ID", block.Header.ID},
			{"Version", strconv.Itoa(int(block.Header.Ver))},
			{"Etag", fmt.Sprintf("%x", block.Header.Etag)},
			{"Total Size", formatSize(block.Header.TotalSize) + " bytes"},
			{"Real Size", formatSize(block.Header.RealSize) + " bytes"},
			{"Compressed", fmt.Sprintf("%t", block.Header.Compressed)},
			{"Encrypted", fmt.Sprintf("%t", block.Header.Encrypted)},
			{"Created At", time.Unix(0, int64(block.Header.CreatedAt)).Format(time.RFC3339)},
			{"Updated At", time.Unix(0, int64(block.Header.UpdatedAt)).Format(time.RFC3339)},
			{"Chunk Count", strconv.Itoa(len(block.Header.ChunkList))},
			{"Data Size", formatSize(int64(len(block.Data))) + " bytes"},
		}

		for i, chunk := range block.Header.ChunkList {
			attrName := fmt.Sprintf("user.dedupfs.chunk.meta.%s", chunk.Hash)
			refCount := int32(0)
			if chunkMetaBytes, err := utils.GetXAttr(mountPoint, attrName); err == nil && len(chunkMetaBytes) > 0 {
				var _chunk dfs.Chunk
				if err := json.Unmarshal(chunkMetaBytes, &_chunk); err == nil {
					refCount = _chunk.RefCount
				}
			}
			chunkLines = append(chunkLines, fmt.Sprintf("%3d: %s, size=%d, ref=%d", i, chunk.Hash, chunk.Size, refCount))
		}
		ChunkList = block.Header.ChunkList

		if block.Header.Encrypted {
			if d, err := utils.Decrypt(block.Data, blockID+blockConf.Password); err != nil {
				return fmt.Errorf("decrypt block failed %w", err)
			} else {
				block.Data = d
			}
		}

		if block.Header.Compressed {
			if d, err := utils.Decompress(block.Data); err != nil {
				return fmt.Errorf("decompress block failed %w", err)
			} else {
				block.Data = d
			}
		}
	}

	// Now both viewports are scrollable
	chunkVP := viewport.New(50, 10)
	// Keep default keymap for scrolling capability

	hexVP := viewport.New(50, 10)

	selectedIndex := -1
	initialHex := "No chunks available."
	if len(chunkLines) > 0 {
		selectedIndex = 0
		initialHex = ""
	}

	m := blockModel{
		infoRows:           infoRows,
		headerRows:         headerRows,
		chunkLines:         chunkLines,
		ChunkList:          ChunkList,
		block:              block,
		mountPoint:         mountPoint,
		selectedChunkIndex: selectedIndex,
		hexContent:         initialHex,
		hexLines:           strings.Split(initialHex, "\n"),
		viewport:           chunkVP,
		hexVP:              hexVP,
		focus:              focusBlockChunk,
		ready:              false,
	}

	if len(chunkLines) > 0 {
		m.loadChunkHex()
		m.scrollToSelectedChunk()
	} else {
		hexVP.SetContent(initialHex)
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
