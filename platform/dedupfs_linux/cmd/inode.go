//go:build linux || darwin

package cmd

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mageg-x/dedupfs/common/dfs"
	"github.com/mageg-x/dedupfs/common/utils"
)

const (
	focusInodeChunk int = iota
	focusInodeHex
)

// inodeModel represents the UI model for inode debugging
type inodeModel struct {
	infoRows   [][]string
	inodeRows  [][]string
	chunkLines []string
	inode      *dfs.INode
	chunkList  []*dfs.INodeChunk
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

// scrollToLine scrolls the viewport to ensure the given lineIndex is visible.
// Compatible with older versions of bubbles/viewport that lack GotoLine.
func (m *inodeModel) scrollToLine(vp *viewport.Model, lineIndex, totalLines int) {
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

// highlightLine highlights the specified line in the hex view
func (m *inodeModel) highlightLine(lines []string, lineIndex, maxWidth int) string {
	if lineIndex < 0 || lineIndex >= len(lines) {
		return strings.Join(lines, "\n")
	}

	// 与Chunk List保持一致的高亮样式
	highlightStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#ffffff"}).
		Background(lipgloss.AdaptiveColor{Light: "#5D5FEF", Dark: "#4A4AEE"})

	newLines := make([]string, len(lines))
	for i, line := range lines {
		if i == lineIndex {
			if len(line) > maxWidth {
				line = line[:maxWidth]
			}
			newLines[i] = highlightStyle.Render(line)
		} else {
			newLines[i] = line
		}
	}
	return strings.Join(newLines, "\n")
}

// loadChunkHex for inode model
func (m *inodeModel) loadChunkHex() {
	if len(m.chunkList) == 0 {
		m.hexContent = "No valid chunks available."
		m.hexLines = strings.Split(m.hexContent, "\n")
		m.hexVP.SetContent(m.hexContent)
		return
	}

	if m.selectedChunkIndex < 0 || m.selectedChunkIndex >= len(m.chunkList) {
		m.hexContent = "Select a chunk to view its hex content."
		m.hexLines = strings.Split(m.hexContent, "\n")
		m.hexVP.SetContent(m.hexContent)
		return
	}

	chunk := m.chunkList[m.selectedChunkIndex]
	if len(chunk.Data) == 0 {
		attrName := fmt.Sprintf("user.dedupfs.chunk.data.%s", chunk.Hash)
		if chunkBytes, err := utils.GetXAttr(m.mountPoint, attrName); err == nil && len(chunkBytes) > 0 {
			m.hexContent = hex.Dump(chunkBytes)
			m.hexLines = strings.Split(m.hexContent, "\n")
			m.hexVP.SetContent(m.hexContent)
			return
		} else {
			logger.Errorf("Failed to get %s", attrName)
		}
	}

	m.hexContent = "Chunk data is not loaded."
	m.hexLines = strings.Split(m.hexContent, "\n")
	m.hexVP.SetContent(m.hexContent)
}

// scrollToSelectedChunk for inode model
func (m *inodeModel) scrollToSelectedChunk() {
	m.scrollToLine(&m.viewport, m.selectedChunkIndex, len(m.chunkLines))
}

// Init for inode model
func (m inodeModel) Init() tea.Cmd {
	return nil
}

// Update for inode model
func (m inodeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case msg.Type == tea.KeyCtrlC, msg.String() == "q", msg.String() == "esc":
			return m, tea.Quit

		case msg.String() == "tab":
			if m.focus == focusInodeChunk {
				m.focus = focusInodeHex
			} else {
				m.focus = focusInodeChunk
			}

		case m.focus == focusInodeChunk:
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

		case m.focus == focusInodeHex:
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
	} else if m.focus == focusInodeHex {
		var cmd tea.Cmd
		m.hexVP, cmd = m.hexVP.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// View for inode model
func (m inodeModel) View() string {
	if !m.ready {
		return "Loading inode data..."
	}

	topBoxHeight := 8 // 减少高度，因为使用3列布局
	chunkSectionTitleHeight := 2
	helpHeight := 2
	availableHeight := m.height - topBoxHeight - chunkSectionTitleHeight - helpHeight
	if availableHeight < 6 {
		availableHeight = 6
	}
	innerHeight := availableHeight - 7
	if innerHeight < 3 {
		innerHeight = 3
	}

	halfWidth := (m.width - 4) / 2
	m.viewport.Width = halfWidth - 2
	m.viewport.Height = innerHeight
	m.hexVP.Width = halfWidth - 2
	m.hexVP.Height = innerHeight

	inodeBoxColor := lipgloss.AdaptiveColor{Light: "#E0569E", Dark: "#F48FB1"}
	chunkBoxColor := lipgloss.AdaptiveColor{Light: "#56E0A6", Dark: "#80CBC4"}
	hexBoxColor := lipgloss.AdaptiveColor{Light: "#E0A656", Dark: "#FFCC80"}
	helpColor := lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}

	inodeBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(inodeBoxColor).
		Padding(1, 2).
		Width(m.width - 4). // 独占整个宽度
		Height(topBoxHeight)

	// 3列渲染函数
	renderRowsInColumns := func(rows [][]string, maxWidth int) string {
		columnWidth := (maxWidth)/3 + 5 // 3列布局，减去间距
		keyWidth := 16
		valWidth := columnWidth - keyWidth - 2

		// 将rows分成3列
		column1 := rows
		column2 := [][]string{}
		column3 := [][]string{}

		// 如果有足够多的行，分成3列
		if len(rows) > 8 {
			thirdPoint := len(rows) * 2 / 3
			column1 = rows[:len(rows)/3]
			column2 = rows[len(rows)/3 : thirdPoint]
			column3 = rows[thirdPoint:]
		} else if len(rows) > 4 {
			// 如果行数适中，分成2列
			column1 = rows[:len(rows)/2]
			column2 = rows[len(rows)/2:]
		}

		// 渲染每一列
		renderColumn := func(col [][]string) []string {
			var lines []string
			for _, r := range col {
				if len(r) != 2 {
					continue
				}
				key := lipgloss.NewStyle().Width(keyWidth).Render(r[0] + ":")
				val := lipgloss.NewStyle().MaxWidth(valWidth).Render(r[1])
				lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, key, val))
			}
			return lines
		}

		col1Lines := renderColumn(column1)
		col2Lines := renderColumn(column2)
		col3Lines := renderColumn(column3)

		// 确保所有列的行数相同
		maxLines := len(col1Lines)
		if len(col2Lines) > maxLines {
			maxLines = len(col2Lines)
		}
		if len(col3Lines) > maxLines {
			maxLines = len(col3Lines)
		}

		// 补齐每一列
		padLines := func(lines []string, max int) []string {
			result := make([]string, max)
			copy(result, lines)
			for i := len(lines); i < max; i++ {
				result[i] = ""
			}
			return result
		}

		col1Lines = padLines(col1Lines, maxLines)
		col2Lines = padLines(col2Lines, maxLines)
		col3Lines = padLines(col3Lines, maxLines)

		// 合并所有列
		var result []string
		for i := 0; i < maxLines; i++ {
			parts := []string{}
			if i < len(col1Lines) && col1Lines[i] != "" {
				parts = append(parts, lipgloss.NewStyle().Width(columnWidth).Render(col1Lines[i]))
			} else {
				parts = append(parts, lipgloss.NewStyle().Width(columnWidth).Render(""))
			}

			if i < len(col2Lines) && col2Lines[i] != "" {
				parts = append(parts, lipgloss.NewStyle().Width(columnWidth).Render(col2Lines[i]))
			} else if len(column2) > 0 {
				parts = append(parts, lipgloss.NewStyle().Width(columnWidth).Render(""))
			}

			if i < len(col3Lines) && col3Lines[i] != "" {
				parts = append(parts, lipgloss.NewStyle().Width(columnWidth).Render(col3Lines[i]))
			} else if len(column3) > 0 {
				parts = append(parts, lipgloss.NewStyle().Width(columnWidth).Render(""))
			}

			if len(parts) > 0 {
				result = append(result, lipgloss.JoinHorizontal(lipgloss.Top, parts...))
			}
		}

		return strings.Join(result, "\n")
	}

	inodeTitle := lipgloss.NewStyle().Foreground(inodeBoxColor).Bold(true).Render("INode Details")

	// 合并infoRows和inodeRows的内容，避免重复
	combinedRows := m.infoRows

	// 添加inodeRows中不存在于infoRows的项
	infoKeys := make(map[string]bool)
	for _, row := range m.infoRows {
		if len(row) > 0 {
			infoKeys[row[0]] = true
		}
	}

	for _, row := range m.inodeRows {
		if len(row) > 0 && !infoKeys[row[0]] {
			combinedRows = append(combinedRows, row)
		}
	}

	topRowContent := inodeTitle + "\n" + renderRowsInColumns(combinedRows, m.width-8) // 8 是边框和内边距
	topRow := inodeBoxStyle.Render(topRowContent)

	// --- Chunk List: 使用与Hex View类似的滚动背景效果，确保选中项始终可见 ---
	// 先确保选中项在可视区域内
	m.scrollToSelectedChunk()

	// 创建显示的chunk行，只高亮选中项
	var displayedChunkLines []string
	highlightStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#ffffff"}).
		Background(lipgloss.AdaptiveColor{Light: "#5D5FEF", Dark: "#4A4AEE"})

	for i, line := range m.chunkLines {
		if i == m.selectedChunkIndex {
			displayedChunkLines = append(displayedChunkLines, highlightStyle.Render(line))
		} else {
			displayedChunkLines = append(displayedChunkLines, line)
		}
	}

	// 设置内容到viewport，让viewport处理滚动
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

	if m.focus == focusInodeChunk {
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

	// 修复边框超出屏幕问题，确保内容适合视图大小
	chunkBoxContent := chunkTitle + "\n" + m.viewport.View()
	hexBoxContent := hexTitle + "\n" + m.hexVP.View()

	// 确保宽度适合屏幕，避免边框超出
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

// debugINodeAction handles the inode debugging functionality
func debugINodeAction(mountPoint, inodeName string) error {
	dataDirBytes, err := utils.GetXAttr(mountPoint, "user.dedupfs.datadir")
	if err != nil {
		logger.Errorf("failed to get user.dedupfs.datadir: %v", err)
		return fmt.Errorf("failed to get user.dedupfs.datadir: %w", err)
	}
	dataDir := string(dataDirBytes)

	inodePath := filepath.Join(mountPoint, inodeName)
	inodePath, err = filepath.Abs(inodePath)
	if err != nil {
		logger.Errorf("failed to get absolute path: %v", err)
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// 通过stat 获取 inode 编号
	stat, err := utils.FsStat(inodePath)
	if err != nil {
		logger.Errorf("failed to get file stat: %v", err)
		return fmt.Errorf("failed to get file stat: %w", err)
	}
	ino := stat.Ino
	attrName := fmt.Sprintf("user.dedupfs.inode.%d", ino)
	inodeBytes, err := utils.GetXAttr(mountPoint, attrName)
	if err != nil {
		logger.Errorf("Failed to get %s user.dedupfs.inode", inodePath)
		return fmt.Errorf("failed to get %s user.dedupfs.inode: %w", inodePath, err)
	}
	var inode dfs.INode
	json.Unmarshal(inodeBytes, &inode)

	// 准备inode信息显示数据
	infoRows := [][]string{
		{"INode Name", inode.Name},
		{"Mount Point", mountPoint},
		{"Data Directory", dataDir},
		{"INode Number", fmt.Sprintf("%d", inode.Ino)},
		{"File Size", formatSize(int64(inode.Size)) + " bytes"},
	}

	inodeRows := [][]string{
		{"Inode Number", fmt.Sprintf("%d", inode.Ino)},
		{"Name", inode.Name},
		{"Parent Inode", fmt.Sprintf("%d", inode.Parent)},
		{"Type", string(inode.Kind)},
		{"Size", formatSize(int64(inode.Size)) + " bytes"},
		{"Blocks", fmt.Sprintf("%d", inode.Blocks)},
		{"Permissions", fmt.Sprintf("%o", inode.Perm)},
		{"Links", fmt.Sprintf("%d", inode.Nlink)},
		{"UID", fmt.Sprintf("%d", inode.Uid)},
		{"GID", fmt.Sprintf("%d", inode.Gid)},
		{"Block Size", fmt.Sprintf("%d", inode.Blksize)},
		{"Flags", fmt.Sprintf("%d", inode.Flags)},
		{"Access Time", inode.Atime.Format(time.RFC3339)},
		{"Modify Time", inode.Mtime.Format(time.RFC3339)},
		{"Change Time", inode.Ctime.Format(time.RFC3339)},
		{"Create Time", inode.Crtime.Format(time.RFC3339)},
		{"Chunk Count", fmt.Sprintf("%d", len(inode.Chunks))},
	}

	if inode.Kind == dfs.FileTypeSymlink && inode.SymlinkTarget != nil {
		inodeRows = append(inodeRows, []string{"Symlink Target", *inode.SymlinkTarget})
	}

	// 准备chunk数据
	var chunkLines []string

	for i, chunk := range inode.Chunks {
		attrName := fmt.Sprintf("user.dedupfs.chunk.meta.%s", chunk.Hash)
		refCount := int32(0)
		size := int32(0)
		if chunkMetaBytes, err := utils.GetXAttr(mountPoint, attrName); err == nil && len(chunkMetaBytes) > 0 {
			var _chunk dfs.Chunk
			if err := json.Unmarshal(chunkMetaBytes, &_chunk); err == nil {
				refCount = _chunk.RefCount
				size = _chunk.Size
			}
		}
		chunkLines = append(chunkLines, fmt.Sprintf("%3d: %s, size=%d, ref=%d", i, chunk.Hash, size, refCount))
	}

	// 初始化模型
	model := inodeModel{
		infoRows:           infoRows,
		inodeRows:          inodeRows,
		chunkLines:         chunkLines,
		chunkList:          inode.Chunks,
		inode:              &inode,
		mountPoint:         mountPoint,
		selectedChunkIndex: 0,
		focus:              focusInodeChunk,
	}

	// 初始化viewport
	model.viewport = viewport.New(50, 10) // 使用合理的初始大小
	model.hexVP = viewport.New(50, 10)

	// 如果有chunk，加载第一个的hex内容
	if len(chunkLines) > 0 {
		model.loadChunkHex()
		model.scrollToSelectedChunk()
	}

	// 启动TUI
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	if err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}

	return nil
}
