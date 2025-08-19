package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const uiWidth = 160
const uiHeight = 40

var (
	focusedStyle = lipgloss.NewStyle().Background(lipgloss.Color("#FFEB3B")).Foreground(lipgloss.Color("#111")).Bold(true)
	normalStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#EEE"))
	tooltipStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Foreground(lipgloss.Color("240")).Width(uiWidth - 4)
)

func updateStatusBars(m *model) {
	// AWS
	awsIcon := ""
	awsStyleOK := lipgloss.NewStyle().Foreground(lipgloss.Color("#44cc11"))  // green
	awsStyleErr := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff4444")) // red
	awsOK, vaultOK := getEnvStatus(m.cfg)
	if awsOK {
		m.awsStatus = awsStyleOK.Render(awsIcon)
	} else {
		m.awsStatus = awsStyleErr.Render(awsIcon)
	}

	// Vault
	vaultIcon := "󰌾"
	vaultStyleOK := lipgloss.NewStyle().Foreground(lipgloss.Color("#44cc11"))  // green
	vaultStyleErr := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff4444")) // red
	if vaultOK {
		m.vaultStatus = vaultStyleOK.Render(vaultIcon)
	} else {
		m.vaultStatus = vaultStyleErr.Render(vaultIcon)
	}

	// Git
	branch, dirty, err := getGitStatus(m.cfg.TerraformPath)
	gitIcon := ""
	gitStyleClean := lipgloss.NewStyle().Foreground(lipgloss.Color("#44cc11")) // green
	gitStyleDirty := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")) // orange
	gitStyleErr := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff4444"))   // red
	if err != nil {
		m.gitStatus = gitStyleErr.Render(fmt.Sprintf("%s ?", gitIcon))
	} else if dirty {
		m.gitStatus = gitStyleDirty.Render(fmt.Sprintf("%s %s", gitIcon, branch))
	} else {
		m.gitStatus = gitStyleClean.Render(fmt.Sprintf("%s %s", gitIcon, branch))
	}
}

// --- UI Constants, Helpers, and Styles ---

var (
	clusterOptions = []string{"cl10400", "cl12600k", "cl12900h", "cl13600k"}
	zoneOptions    = []string{"standard", "admin", "dmz"}
)

func cycleOption(current string, options []string, dir int) string {
	for i, opt := range options {
		if opt == current {
			newIdx := (i + dir + len(options)) % len(options)
			return options[newIdx]
		}
	}
	return options[0]
}

type scene int

const (
	sceneLauncher scene = iota
	sceneCreateForm
	sceneEditTable
	sceneEditForm
	sceneConfirmDestroy
)

type model struct {
	cfg           Config
	presets       []Preset
	presetIdx     int
	fieldMeta     map[string]FieldMeta
	helpText      string
	currentScene  scene
	statusMessage string

	createInputs []textinput.Model
	createLabels []string
	createFocus  int

	deployments []deploymentInfo

	editStatus string

	editFormInputs []textinput.Model
	editFormLabels []string
	editFormPath   string
	editFocusIndex int

	gitStatus   string
	awsStatus   string
	vaultStatus string

	deployTable table.Model
	tfvarsTable table.Model

	templatesForCluster []string
	// Optionally, a busy flag/loading state for UX
	isFetchingTemplates bool

	// --- NEW FIELDS ---
	isBusy bool

	// Destroy confirmation
	pendingDestroyIdx  int
	pendingDestroyName string
	pendingDestroyPath string
}

func (m model) Init() tea.Cmd {
	return nil
}

func main() {
	cfg, err := loadConfig("config.yaml")
	if err != nil {
		fmt.Println("ERROR: could not load config.yaml:", err)
		os.Exit(1)
	}
	// Optional: load cluster/zone options from clusters.yaml if present
	if _, err := os.Stat("clusters.yaml"); err == nil {
		if opts, err := loadOptions("clusters.yaml"); err == nil {
			if len(opts.Clusters) > 0 {
				clusterOptions = opts.Clusters
			}
			if len(opts.Zones) > 0 {
				zoneOptions = opts.Zones
			}
		}
	}
	presets, err := loadPresets(cfg.PresetsPath)
	if err != nil {
		fmt.Println("ERROR: could not load presets from presets dir:", err)
		os.Exit(1)
	}
	if len(presets) == 0 {
		fmt.Println("No presets found in presets dir!")
		os.Exit(1)
	}
	fieldMeta, err := loadFieldMeta("fields.yaml")
	if err != nil {
		fmt.Println("ERROR: could not load fields.yaml:", err)
		os.Exit(1)
	}
	m := initialModel(cfg, presets, fieldMeta)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		log.Fatal(err)
	}
}

func initialModel(cfg Config, presets []Preset, fieldMeta map[string]FieldMeta) model {
	labels := []string{
		"vm_app", "platform_description", "zone", "platform_id", "vm_network_suffix", "vm_id_prefix",
		"vm_memory", "vm_cpu_cores", "vm_disk_count", "vm_disk_size", "vm_count", "vm_template",
		"cluster",
	}

	// Deployments table
	deployCols := []table.Column{
		{Title: "Name", Width: 24},
		{Title: "Description", Width: 32},
		{Title: "State", Width: 13},
		{Title: "Last Action", Width: 20},
	}
	deployInfos, _ := listDeployments(cfg.AppsPath)
	deployRows := make([]table.Row, len(deployInfos))
	for i, info := range deployInfos {
		deployRows[i] = table.Row{info.Name, info.Description, info.State, info.LastAction}
	}
	deployTable := table.New(
		table.WithColumns(deployCols),
		table.WithRows(deployRows),
		table.WithFocused(true),
	)
	deployTable.SetHeight(20)

	tfvarsTable := loadTfvarsTableForDeployment(
		cfg.AppsPath,
		deployInfos,
		0, // show first deployment at launch
		fieldMeta,
	)

	inputs := make([]textinput.Model, len(labels))
	presetIdx := 0
	for i, name := range labels {
		ti := textinput.New()
		ti.Placeholder = name
		if val, ok := presets[presetIdx].Values[name]; ok {
			switch v := val.(type) {
			case string:
				ti.SetValue(v)
			case int:
				ti.SetValue(fmt.Sprintf("%d", v))
			case []interface{}:
				strs := []string{}
				for _, e := range v {
					strs = append(strs, fmt.Sprintf("%v", e))
				}
				ti.SetValue(strings.Join(strs, ","))
			default:
				ti.SetValue(fmt.Sprintf("%v", v))
			}
		}
		inputs[i] = ti
	}
	// === ADD THIS SNIPPET BELOW ===
	templateIdx := indexOf("vm_template", labels)
	if templateIdx >= 0 {
		inputs[templateIdx].Width = 40 // pick the width you want!
	}
	// ==============================
	inputs[0].Focus()

	m := model{
		cfg:            cfg,
		presets:        presets,
		presetIdx:      0,
		currentScene:   sceneLauncher,
		createInputs:   inputs,
		createLabels:   labels,
		createFocus:    0,
		fieldMeta:      fieldMeta,
		helpText:       "",
		editFormLabels: []string{"vm_cpu_cores", "vm_memory", "vm_count", "vm_disk_count", "vm_disk_size"},
		deployments:    deployInfos,
		deployTable:    deployTable,
		tfvarsTable:    tfvarsTable,
	}

	updateStatusBars(&m) // ← THIS IS ALL YOU NEED
	return m
}

// --- UI Rendering ---
func (m model) View() string {
	var header, body, tooltip, footer string

	status := padLeft(fmt.Sprintf("%s  %s  %s", m.awsStatus, m.vaultStatus, m.gitStatus), uiWidth+65-len("Infrastructure Catalog"))

	// ---- HEADER (bubbles/box style) ----
	headerText := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("81")). // Light blue
		Render("Infrastructure Catalog")
	header = tooltipStyle.Render(centerText(headerText, uiWidth-len(status)) + status)
	header += "\n" + " " + strings.Repeat("─", uiWidth-4) + "\n"

	// ---- BODY (scene switch) ----
	switch m.currentScene {
	case sceneLauncher:
		deployTableStr := m.deployTable.View()
		selected := m.deployTable.Cursor()
		col1Width := 89
		col2Width := 68
		// Render non-scrollable details for the selected deployment
		detailsStr := renderDetailsPanel(m.cfg.AppsPath, m.deployments, selected, m.fieldMeta, col2Width, 20)
		lines1 := strings.Split(deployTableStr, "\n")
		lines2 := strings.Split(detailsStr, "\n")
		maxLines := max(len(lines1), len(lines2))
		for len(lines1) < maxLines {
			lines1 = append(lines1, strings.Repeat(" ", col1Width))
		}
		for len(lines2) < maxLines {
			lines2 = append(lines2, strings.Repeat(" ", col2Width))
		}
		var out string
		for i := 0; i < maxLines; i++ {
			out += padRight(lines1[i], col1Width) + " │ " + padRight(lines2[i], col2Width) + "\n"
		}
		body = out
		tooltip = tooltipStyle.Render(m.statusMessage)
	case sceneCreateForm:
		body += tooltipStyle.Render(fmt.Sprintf("[Preset: %s] (F2/F3 to switch)", m.presets[m.presetIdx].Name))
		body += "\n" + " " + strings.Repeat("─", uiWidth-4) + "\n"
		for i, ti := range m.createInputs {
			cursor := " "
			isFocused := i == m.createFocus
			label := m.fieldMeta[m.createLabels[i]].Label
			val := ti.Value()
			display := padRight(val, 38)
			field := ""
			if isFocused {
				field = focusedStyle.Render(fmt.Sprintf("%s %-25s: > %s", cursor, label, display))
			} else {
				field = normalStyle.Render(fmt.Sprintf("%s %-25s: > %s", cursor, label, display))
			}
			body += field + "\n"
		}
		tooltip = tooltipStyle.Render(m.fieldMeta[m.createLabels[m.createFocus]].Help)
	case sceneEditForm:
		for i, ti := range m.editFormInputs {
			cursor := " "
			isFocused := i == m.editFocusIndex
			label := m.fieldMeta[m.editFormLabels[i]].Label
			val := ti.Value()
			display := padRight(val, 38)
			field := ""
			if isFocused {
				field = focusedStyle.Render(fmt.Sprintf("%s %-25s: > %s", cursor, label, display))
			} else {
				field = normalStyle.Render(fmt.Sprintf("%s %-25s: > %s", cursor, label, display))
			}
			body += field + "\n"
		}
		if m.editStatus != "" {
			tooltip = tooltipStyle.Render(m.editStatus)
		} else {
			tooltip = tooltipStyle.Render(m.fieldMeta[m.editFormLabels[m.editFocusIndex]].Help)
		}
	case sceneConfirmDestroy:
		// Confirmation view
		title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render("Confirm Destroy")
		body = "\n" + boxSection(centerText(title, uiWidth-4)) + "\n"
		list := fmt.Sprintf("1) terraform destroy\n2) delete remote state prefix s3://%s/%s/\n3) remove %s directory\n\nContinue?",
			m.cfg.S3Bucket, filepath.Base(m.pendingDestroyPath), m.pendingDestroyName)
		body += tooltipStyle.Render(list)
		// No tooltip here; options are shown in footer only to avoid duplicate boxes
		tooltip = ""
	default:
		body, tooltip = "", ""
	}

	// ---- FOOTER: scene-dependent ----
	footer = footerForScene(m)

	// ---- BOX WRAP ----
	var result strings.Builder
	result.WriteString(header)
	result.WriteString(body)
	result.WriteString(tooltip)

	// Vertical padding so footer stays at the bottom
	linesSoFar := countLines(header) + countLines(body) + countLines(tooltip)
	footerLines := countLines(footer)
	boxBottomLines := 1
	paddingLines := uiHeight - linesSoFar - footerLines - boxBottomLines
	if paddingLines < 0 {
		paddingLines = 0
	}
	result.WriteString(strings.Repeat("\n", paddingLines))
	result.WriteString("\n") // <-- Adds a blank line between tooltip and footer
	result.WriteString(boxSection(footer))
	return result.String()
}

// Use this for your scene-based footer logic
func footerForScene(m model) string {
	switch m.currentScene {
	case sceneLauncher:
		return centerText("[↑/↓] Field  │  [N] New  │  [A] Apply  │  [U] Update  │  [D] Destroy  │  [R] Refresh  │  [Esc] Cancel", uiWidth)
	case sceneCreateForm:
		return centerText("[↑/↓] Field │ [Tab] Next │ [Enter] Save │ [Esc] Cancel", uiWidth)
	case sceneEditForm:
		return centerText("[↑/↓] Field │ [Tab] Next │ [Enter] Save │ [A] Apply │ [Esc] Cancel", uiWidth)
	case sceneConfirmDestroy:
		keyStyle := lipgloss.NewStyle().Bold(true)
		opt := fmt.Sprintf("[%s] Yes │ [%s] Plan destroy │ [%s] Cancel",
			keyStyle.Render("y"), keyStyle.Render("p"), keyStyle.Render("n/Esc"))
		return centerText(opt, uiWidth)
	default:
		return centerText("", uiWidth)
	}
}

func boxSection(content string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Width(uiWidth - 4).
		PaddingLeft(2).PaddingRight(2).
		Render(content)
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func centerText(s string, width int) string {
	if len(s) >= width {
		return s
	}
	padding := (width - len(s)) / 2
	return strings.Repeat(" ", padding) + s + strings.Repeat(" ", width-len(s)-padding)
}
func padLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}
func countLines(s string) int {
	return strings.Count(s, "\n")
}

// func tableHeader(cols []string, widths []int, totalWidth int) string {
// 	row := ""
// 	for i, c := range cols {
// 		row += " " + c + strings.Repeat(" ", widths[i]-len(c)) + "│"
// 	}
// 	spacing := totalWidth - len(row)
// 	if spacing > 0 {
// 		row += strings.Repeat(" ", spacing)
// 	}
// 	return row + "\n" + strings.Repeat("-", totalWidth) + "\n"
// }
// func tooltipBox(msg string) string {
// 	return tooltipStyle.Render("\n Tooltip\n" + strings.Repeat("─", uiWidth-4) + "\n" + msg + "\n")
// }

// func navFooter() string {
// 	return centerText("[↑/↓] Field │ [Tab] Next │ [Enter] Save │ [A] Apply │ [Esc] Cancel", uiWidth)
// }

// Loads tfvars for the selected deployment index, from real data
func loadTfvarsTableForDeployment(appsPath string, infos []deploymentInfo, idx int, fieldMeta map[string]FieldMeta) table.Model {
	tfvarsCols := []table.Column{
		{Title: "Field", Width: 28},
		{Title: "Value", Width: 35},
	}
	var tfvarsRows []table.Row
	if idx >= 0 && idx < len(infos) {
		tfvars, _ := loadTfvars(filepath.Join(infos[idx].Path, "terraform.tfvars"))
		for k, v := range tfvars {
			label := k
			if meta, ok := fieldMeta[k]; ok && meta.Label != "" {
				label = meta.Label
			}
			tfvarsRows = append(tfvarsRows, table.Row{label, v})
		}
	}
	tfvarsTable := table.New(
		table.WithColumns(tfvarsCols),
		table.WithRows(tfvarsRows),
		table.WithFocused(false),
	)
	tfvarsTable.SetHeight(20)
	return tfvarsTable
}

// renderDetailsPanel formats the selected deployment's tfvars as non-scrollable text.
func renderDetailsPanel(appsPath string, infos []deploymentInfo, idx int, fieldMeta map[string]FieldMeta, width int, maxHeight int) string {
	if idx < 0 || idx >= len(infos) {
		return strings.Repeat(" ", width)
	}
	tfvars, _ := loadTfvars(filepath.Join(infos[idx].Path, "terraform.tfvars"))
	// Deterministic ordering: sort by label
	type kv struct{ k, v, label string }
	var rows []kv
	for k, v := range tfvars {
		label := k
		if meta, ok := fieldMeta[k]; ok && meta.Label != "" {
			label = meta.Label
		}
		rows = append(rows, kv{k: k, v: v, label: label})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].label < rows[j].label })
	var b strings.Builder
	// Header
	b.WriteString(padRight("Details", width))
	b.WriteString("\n")
	count := 0
	for _, r := range rows {
		line := fmt.Sprintf("%-28s %s", r.label+":", r.v)
		if len(line) > width {
			line = line[:width]
		}
		b.WriteString(padRight(line, width))
		b.WriteString("\n")
		count++
		if count >= maxHeight-2 { // leave room for header spacing
			break
		}
	}
	return b.String()
}

// --- Update logic: only allow quit during isBusy
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.isBusy {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "q", "esc":
				return m, tea.Quit
			default:
				return m, nil
			}
		default:
			return m, nil
		}
	}
	switch m.currentScene {
	case sceneLauncher:
		return updateLauncher(m, msg)
	case sceneCreateForm:
		return updateCreateForm(m, msg)
	case sceneEditForm:
		return updateEditForm(m, msg)
	case sceneConfirmDestroy:
		return updateConfirmDestroy(m, msg)
	}
	return m, nil
}

func updateLauncher(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k", "down", "j":
			var cmd tea.Cmd
			m.deployTable, cmd = m.deployTable.Update(msg)
			selected := m.deployTable.Cursor()
			m.tfvarsTable = loadTfvarsTableForDeployment(m.cfg.AppsPath, m.deployments, selected, m.fieldMeta)
			return m, cmd
		case "n":
			m.currentScene = sceneCreateForm
			return m, nil
		case "enter", "e":
			idx := m.deployTable.Cursor()
			if idx >= 0 && idx < len(m.deployments) {
				dep := m.deployments[idx]
				tfvars := filepath.Join(dep.Path, "terraform.tfvars")
				vals, err := loadTfvars(tfvars)
				if err != nil {
					m.editStatus = "Could not load tfvars: " + err.Error()
					return m, nil
				}
				// Build edit form with only editable fields
				inputs, labels := buildEditFormInputs(vals, m.fieldMeta, m.createLabels)
				inputs[0].Focus()
				m.editFormInputs = inputs
				m.editFormLabels = labels
				m.editFormPath = tfvars
				m.editFocusIndex = 0
				m.currentScene = sceneEditForm
				return m, nil
			}
		case "d", "D":
			idx := m.deployTable.Cursor()
			if idx >= 0 && idx < len(m.deployments) {
				dep := m.deployments[idx]
				m.pendingDestroyIdx = idx
				m.pendingDestroyName = dep.Name
				m.pendingDestroyPath = dep.Path
				m.currentScene = sceneConfirmDestroy
				return m, nil
			}
		case "q", "esc":
			return m, tea.Quit
		case "r", "R":
			m.statusMessage = "Refreshing deployments..."
			deployments, _ := listDeployments(m.cfg.AppsPath)
			m.deployments = deployments
			// Refresh deployTable and tfvarsTable as needed
			deployRows := make([]table.Row, len(deployments))
			for i, info := range deployments {
				deployRows[i] = table.Row{info.Name, info.Description, info.State, info.LastAction}
			}
			m.deployTable.SetRows(deployRows)
			m.tfvarsTable = loadTfvarsTableForDeployment(m.cfg.AppsPath, deployments, 0, m.fieldMeta)
			// Refresh status bars in-place
			updateStatusBars(&m)
			m.statusMessage = "Deployments refreshed!"
			return m, nil

		}
	}
	return m, nil
}

func updateConfirmDestroy(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "enter":
			// Proceed with destroy
			if m.pendingDestroyIdx >= 0 && m.pendingDestroyIdx < len(m.deployments) {
				dep := m.deployments[m.pendingDestroyIdx]
				m.statusMessage = "Running terraform destroy..."
				if err := runTerraformDestroy(dep.Path); err != nil {
					m.statusMessage = "terraform destroy failed: " + err.Error()
					m.currentScene = sceneLauncher
					return m, nil
				}
				_ = setDeploymentState(dep.Path, "DESTROYED", "destroy")
				// Remove S3 prefix (best-effort)
				appDir := filepath.Base(dep.Path)
				_ = deleteS3Prefix(m.cfg.S3Bucket, fmt.Sprintf("%s/", appDir), m.cfg.AWSProfile, m.cfg.AWSRegion)
				// Remove local directory (best-effort)
				if err := os.RemoveAll(dep.Path); err != nil {
					m.statusMessage = "Destroyed, but failed to delete directory: " + err.Error()
				} else {
					m.statusMessage = "Destroyed: terraform + remote state + directory removed."
				}
				// Refresh
				deployments, _ := listDeployments(m.cfg.AppsPath)
				m.deployments = deployments
				deployRows := make([]table.Row, len(deployments))
				for i, info := range deployments {
					deployRows[i] = table.Row{info.Name, info.Description, info.State, info.LastAction}
				}
				m.deployTable.SetRows(deployRows)
				m.tfvarsTable = loadTfvarsTableForDeployment(m.cfg.AppsPath, deployments, 0, m.fieldMeta)
			}
			m.currentScene = sceneLauncher
			return m, nil
		case "p":
			// Dry-run plan
			if m.pendingDestroyIdx >= 0 && m.pendingDestroyIdx < len(m.deployments) {
				dep := m.deployments[m.pendingDestroyIdx]
				if err := runTerraformPlanDestroy(dep.Path); err != nil {
					m.statusMessage = "plan -destroy failed: " + err.Error()
				} else {
					m.statusMessage = "plan -destroy completed successfully."
				}
			}
			// Keep user in confirm view after plan
			return m, nil
		case "n", "esc":
			// Cancel destroy
			m.statusMessage = "Destroy canceled."
			m.currentScene = sceneLauncher
			return m, nil
		}
	}
	return m, nil
}

func (m model) withScene(s scene) model {
	m.currentScene = s
	return m
}

func buildEditFormInputs(tfvars map[string]string, fieldMeta map[string]FieldMeta, orderedFields []string) ([]textinput.Model, []string) {
	var labels []string
	for _, key := range orderedFields {
		if !fieldMeta[key].ReadOnly {
			labels = append(labels, key)
		}
	}
	inputs := make([]textinput.Model, len(labels))
	for i, key := range labels {
		ti := textinput.New()
		ti.Placeholder = key
		val := tfvars[key]
		val = strings.Trim(val, "\"[]")
		ti.SetValue(val)
		inputs[i] = ti
	}
	return inputs, labels
}

// Utility: find index in your createLabels slice
func indexOf(label string, labels []string) int {
	for i, l := range labels {
		if l == label {
			return i
		}
	}
	return -1
}

// Message type for when templates are fetched (async)
type templatesFetchedMsg struct {
	templates []string
	err       error
}

// Async fetch function as a Bubbletea command
func fetchTemplatesCmd(cluster string) tea.Cmd {
	return func() tea.Msg {
		templates, err := fetchTemplatesForCluster(cluster)
		return templatesFetchedMsg{templates, err}
	}
}

type BusyFinishedMsg struct {
	Success      bool
	ErrorMessage string
}

func applyPresetToForm(m model, presetIdx int) model {
	for i, label := range m.createLabels {
		val, ok := m.presets[presetIdx].Values[label]
		if ok {
			switch v := val.(type) {
			case string:
				m.createInputs[i].SetValue(v)
			case int:
				m.createInputs[i].SetValue(fmt.Sprintf("%d", v))
			case []interface{}:
				strs := []string{}
				for _, e := range v {
					strs = append(strs, fmt.Sprintf("%v", e))
				}
				m.createInputs[i].SetValue(strings.Join(strs, ","))
			default:
				m.createInputs[i].SetValue(fmt.Sprintf("%v", v))
			}
		}
	}
	return m
}

// Replace your updateCreateForm with:
func updateCreateForm(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
	// Index helpers
	clusterIdx, templateIdx := -1, -1
	for i, label := range m.createLabels {
		if label == "cluster" {
			clusterIdx = i
		}
		if label == "vm_template" {
			templateIdx = i
		}
	}
	curLabel := m.createLabels[m.createFocus]

	// Fields that are read-only for typing (cycling only)
	readonlyFields := map[string]bool{
		"zone":        true,
		"cluster":     true,
		"vm_template": true,
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Make these fields only cycle with left/right/space, block text input
		if readonlyFields[curLabel] {
			switch msg.String() {
			case "left":
				switch curLabel {
				case "zone":
					cur := m.createInputs[m.createFocus].Value()
					m.createInputs[m.createFocus].SetValue(cycleOption(cur, zoneOptions, -1))
				case "cluster":
					cur := m.createInputs[clusterIdx].Value()
					newCluster := cycleOption(cur, clusterOptions, -1)
					m.createInputs[clusterIdx].SetValue(newCluster)
					m.isFetchingTemplates = true
					return m, fetchTemplatesCmd(newCluster)
				case "vm_template":
					if len(m.templatesForCluster) > 0 {
						cur := m.createInputs[templateIdx].Value()
						m.createInputs[templateIdx].SetValue(cycleOption(cur, m.templatesForCluster, -1))
					}
				}
			case "right", " ":
				switch curLabel {
				case "zone":
					cur := m.createInputs[m.createFocus].Value()
					m.createInputs[m.createFocus].SetValue(cycleOption(cur, zoneOptions, +1))
				case "cluster":
					cur := m.createInputs[clusterIdx].Value()
					newCluster := cycleOption(cur, clusterOptions, +1)
					m.createInputs[clusterIdx].SetValue(newCluster)
					m.isFetchingTemplates = true
					return m, fetchTemplatesCmd(newCluster)
				case "vm_template":
					if len(m.templatesForCluster) > 0 {
						cur := m.createInputs[templateIdx].Value()
						m.createInputs[templateIdx].SetValue(cycleOption(cur, m.templatesForCluster, +1))
					}
				}
			case "tab":
				m.createFocus = (m.createFocus + 1) % len(m.createInputs)
			case "shift+tab":
				m.createFocus = (m.createFocus - 1 + len(m.createInputs)) % len(m.createInputs)
			case "up":
				m.createFocus = (m.createFocus - 1 + len(m.createInputs)) % len(m.createInputs)
			case "down":
				m.createFocus = (m.createFocus + 1) % len(m.createInputs)
			case "esc", "ctrl+c":
				return m.withScene(sceneLauncher), nil
			case "f2":
				m.presetIdx = (m.presetIdx - 1 + len(m.presets)) % len(m.presets)
				m = applyPresetToForm(m, m.presetIdx)
				return m, nil
			case "f3":
				m.presetIdx = (m.presetIdx + 1) % len(m.presets)
				m = applyPresetToForm(m, m.presetIdx)
				return m, nil
			case "enter":
				// You may want to allow enter to submit even if focus is on a cycling field
				break // let it fall through below
			default:
				// Ignore typing input for these fields
				return m, nil
			}
		} else {
			// Handle non-readonly fields as normal
			switch msg.String() {
			case "tab":
				m.createFocus = (m.createFocus + 1) % len(m.createInputs)
			case "shift+tab":
				m.createFocus = (m.createFocus - 1 + len(m.createInputs)) % len(m.createInputs)
			case "up":
				m.createFocus = (m.createFocus - 1 + len(m.createInputs)) % len(m.createInputs)
			case "down":
				m.createFocus = (m.createFocus + 1) % len(m.createInputs)
			case "esc", "ctrl+c":
				return m.withScene(sceneLauncher), nil
			}
		}

		// Save/deploy logic (always allowed on Enter)
		if msg.String() == "enter" {
			provider := "proxmox"
			app := m.createInputs[indexOf("vm_app", m.createLabels)].Value()
			zone := m.createInputs[indexOf("zone", m.createLabels)].Value()
			platformID := m.createInputs[indexOf("platform_id", m.createLabels)].Value()
			appDir := fmt.Sprintf("%s_%s_%s_%s", provider, app, zone, platformID)
			destPath := filepath.Join(m.cfg.AppsPath, appDir)

			if _, err := os.Stat(destPath); err == nil {
				m.statusMessage = fmt.Sprintf("Deployment '%s' already exists!", appDir)
				return m, nil
			}
			if err := copyDir(m.cfg.TemplatePath, destPath); err != nil {
				m.statusMessage = "Failed to copy template: " + err.Error()
				return m, nil
			}
			updates := make(map[string]string)
			stringFields := map[string]bool{
				"platform_description": true,
				"vm_app":               true,
				"zone":                 true,
				"cluster":              true,
				"platform_id":          true,
				"vm_template":          true,
			}
			for i, key := range m.createLabels {
				v := m.createInputs[i].Value()
				if key == "vm_disk_size" {
					arr := []string{}
					for _, part := range strings.Split(v, ",") {
						s := strings.Trim(strings.TrimSpace(part), "\"")
						arr = append(arr, fmt.Sprintf("\"%s\"", s))
					}
					updates[key] = "[" + strings.Join(arr, ", ") + "]"
				} else if stringFields[key] {
					updates[key] = fmt.Sprintf("\"%s\"", v)
				} else {
					updates[key] = v
				}
			}
			tfvarsPath := filepath.Join(destPath, "terraform.tfvars")
			if err := saveTfvars(tfvarsPath, updates); err != nil {
				m.statusMessage = "Failed to write tfvars: " + err.Error()
				return m, nil
			}
			regionLine := "ap-southeast-2"
			if m.cfg.AWSRegion != "" {
				regionLine = m.cfg.AWSRegion
			}
			profileLine := ""
			if m.cfg.AWSProfile != "" {
				profileLine = fmt.Sprintf("\n    profile         = \"%s\"", m.cfg.AWSProfile)
			}
			s3tf := fmt.Sprintf(
				`terraform {
  backend "s3" {
    bucket          = "%s"
    key             = "%s/s3/terraform.tfstate"
    use_lockfile    = true
    region          = "%s"
    encrypt         = true%s
  }
}
`, m.cfg.S3Bucket, appDir, regionLine, profileLine)
			s3tfPath := filepath.Join(destPath, "s3.tf")
			if err := os.WriteFile(s3tfPath, []byte(s3tf), 0644); err != nil {
				m.statusMessage = "Failed to write s3.tf: " + err.Error()
				return m, nil
			}
			if err := setDeploymentState(destPath, "READY", "save"); err != nil {
				m.statusMessage = "Failed to write launcher.state: " + err.Error()
				return m, nil
			}
			// Terraform actions
			m.statusMessage = fmt.Sprintf("Deployment '%s' created. Running terraform init...", appDir)
			if err := runTerraformInit(destPath); err != nil {
				m.statusMessage = "terraform init failed: " + err.Error()
				return m, nil
			}
			if err := setDeploymentState(destPath, "INITIALIZED", "init"); err != nil {
				m.statusMessage = "Failed to update launcher.state (init): " + err.Error()
				return m, nil
			}
			m.statusMessage = fmt.Sprintf("Deployment '%s' initialized. Running terraform apply...", appDir)
			if err := runTerraformApply(destPath); err != nil {
				m.statusMessage = "terraform apply failed: " + err.Error()
				return m, nil
			}
			if err := setDeploymentState(destPath, "DEPLOYED", "apply"); err != nil {
				m.statusMessage = "Failed to update launcher.state (apply): " + err.Error()
				return m, nil
			}
			m.statusMessage = fmt.Sprintf("Deployment '%s' deployed and ready!", appDir)
			return m.withScene(sceneLauncher), nil
		}

		// Focus/blur for all fields
		for i := range m.createInputs {
			if i == m.createFocus {
				m.createInputs[i].Focus()
			} else {
				m.createInputs[i].Blur()
			}
		}
	case templatesFetchedMsg:
		m.isFetchingTemplates = false
		if msg.err != nil {
			m.statusMessage = "Could not fetch templates: " + msg.err.Error()
			m.templatesForCluster = nil
		} else {
			m.templatesForCluster = msg.templates
			// Set template field to first available if not empty
			if templateIdx >= 0 && len(msg.templates) > 0 {
				m.createInputs[templateIdx].SetValue(msg.templates[0])
			} else if templateIdx >= 0 {
				m.createInputs[templateIdx].SetValue("")
			}
		}
		return m, nil
	}
	// Update textinputs
	var cmds []tea.Cmd
	for i := range m.createInputs {
		ti, cmd := m.createInputs[i].Update(msg)
		m.createInputs[i] = ti
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// getEnvStatus now lives in internal_config.go

func updateEditForm(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		curLabel := m.editFormLabels[m.editFocusIndex]
		switch msg.String() {
		case "esc", "q":
			return m.withScene(sceneLauncher), nil
		case "tab":
			m.editFocusIndex = (m.editFocusIndex + 1) % len(m.editFormInputs)
		case "shift+tab":
			m.editFocusIndex = (m.editFocusIndex - 1 + len(m.editFormInputs)) % len(m.editFormInputs)
		case "up":
			m.editFocusIndex = (m.editFocusIndex - 1 + len(m.editFormInputs)) % len(m.editFormInputs)
		case "down":
			m.editFocusIndex = (m.editFocusIndex + 1) % len(m.editFormInputs)
		case "left":
			if curLabel == "zone" {
				cur := m.editFormInputs[m.editFocusIndex].Value()
				m.editFormInputs[m.editFocusIndex].SetValue(cycleOption(cur, zoneOptions, -1))
			} else if curLabel == "cluster" {
				cur := m.editFormInputs[m.editFocusIndex].Value()
				m.editFormInputs[m.editFocusIndex].SetValue(cycleOption(cur, clusterOptions, -1))
			}
		case "right":
			if curLabel == "zone" {
				cur := m.editFormInputs[m.editFocusIndex].Value()
				m.editFormInputs[m.editFocusIndex].SetValue(cycleOption(cur, zoneOptions, +1))
			} else if curLabel == "cluster" {
				cur := m.editFormInputs[m.editFocusIndex].Value()
				m.editFormInputs[m.editFocusIndex].SetValue(cycleOption(cur, clusterOptions, +1))
			}
		case " ":
			if curLabel == "zone" {
				cur := m.editFormInputs[m.editFocusIndex].Value()
				m.editFormInputs[m.editFocusIndex].SetValue(cycleOption(cur, zoneOptions, +1))
			} else if curLabel == "cluster" {
				cur := m.editFormInputs[m.editFocusIndex].Value()
				m.editFormInputs[m.editFocusIndex].SetValue(cycleOption(cur, clusterOptions, +1))
			}
		case "enter":
			// Save tfvars only
			updates := make(map[string]string)
			for i, key := range m.editFormLabels {
				v := m.editFormInputs[i].Value()
				meta := m.fieldMeta[key]
				if key == "vm_disk_size" {
					arr := []string{}
					for _, part := range strings.Split(v, ",") {
						s := strings.Trim(strings.TrimSpace(part), "\"")
						arr = append(arr, fmt.Sprintf("\"%s\"", s))
					}
					updates[key] = "[" + strings.Join(arr, ", ") + "]"
				} else if meta.Type == "string" {
					updates[key] = fmt.Sprintf("\"%s\"", v)
				} else {
					updates[key] = v
				}
			}
			if err := saveTfvars(m.editFormPath, updates); err != nil {
				m.editStatus = "Save failed: " + err.Error()
			} else {
				m.editStatus = "Saved! (You may now apply changes as needed.)"
			}
			return m, nil
		case "a": // [A] Apply
			deployDir := filepath.Dir(m.editFormPath)
			m.editStatus = "Running terraform apply..."
			if err := runTerraformInit(deployDir); err != nil {
				m.editStatus = "terraform init failed: " + err.Error()
				return m, nil
			}
			if err := setDeploymentState(deployDir, "INITIALIZED", "init"); err != nil {
				m.editStatus = "Failed to update launcher.state (init): " + err.Error()
				return m, nil
			}
			if err := runTerraformApply(deployDir); err != nil {
				m.editStatus = "terraform apply failed: " + err.Error()
				return m, nil
			}
			if err := setDeploymentState(deployDir, "DEPLOYED", "apply"); err != nil {
				m.editStatus = "Failed to update launcher.state (apply): " + err.Error()
				return m, nil
			}
			m.editStatus = "Deployment applied and ready!"
			return m, nil
		}
		for i := range m.editFormInputs {
			if i == m.editFocusIndex {
				m.editFormInputs[i].Focus()
			} else {
				m.editFormInputs[i].Blur()
			}
		}
	}
	var cmds []tea.Cmd
	for i := range m.editFormInputs {
		ti, cmd := m.editFormInputs[i].Update(msg)
		m.editFormInputs[i] = ti
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
