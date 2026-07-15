package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	t "github.com/Hunvreus-wiki/skubell/internal/i18n"
)

const (
	workflowStepSelection = iota
	workflowStepOptions
	workflowStepVerification
	workflowStepExecution
)

const workflowStepCount = 4

// workflowStepName returns the translated name of a workflow step.
func workflowStepName(step int) string {
	switch step {
	case workflowStepSelection:
		return t.T("workflow_step_selection", "Selection")
	case workflowStepOptions:
		return t.T("workflow_step_options", "Options")
	case workflowStepVerification:
		return t.T("workflow_step_verification", "Verification")
	case workflowStepExecution:
		return t.T("workflow_step_execution", "Execution")
	default:
		return ""
	}
}

// workflowButtonState controls the enabled state and label of navigation buttons.
type workflowButtonState struct {
	BackEnabled    bool
	HomeEnabled    bool
	CancelEnabled  bool
	ProceedEnabled bool
	ProceedLabel   string
}

// workflowController renders reusable workflow chrome (step title + nav buttons).
type workflowController struct {
	app           *App
	titleLabel    *widget.Label
	stepLabel     *widget.Label
	content       *fyne.Container
	backButton    *widget.Button
	homeButton    *widget.Button
	cancelButton  *widget.Button
	proceedButton *widget.Button
	root          fyne.CanvasObject

	onBack    func()
	onHome    func()
	onCancel  func()
	onProceed func()
}

func newWorkflowController(app *App, onBack, onHome, onCancel, onProceed func()) *workflowController {
	w := &workflowController{
		app:       app,
		onBack:    onBack,
		onHome:    onHome,
		onCancel:  onCancel,
		onProceed: onProceed,
	}
	w.root = w.build()
	return w
}

func (w *workflowController) build() fyne.CanvasObject {
	w.titleLabel = widget.NewLabelWithStyle(
		t.T("workflow_delete_pages", "Delete pages"),
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)
	w.stepLabel = widget.NewLabel("")
	w.SetStep(workflowStepSelection)

	w.backButton = widget.NewButtonWithIcon(t.T("common_back", "Back"), theme.NavigateBackIcon(), func() {
		if w.onBack != nil {
			w.onBack()
		}
	})
	w.homeButton = widget.NewButtonWithIcon(t.T("common_home", "Home"), theme.HomeIcon(), func() {
		if w.onHome != nil {
			w.onHome()
		}
	})
	w.cancelButton = widget.NewButton(t.T("common_cancel", "Cancel"), func() {
		if w.onCancel != nil {
			w.onCancel()
		}
	})
	w.proceedButton = widget.NewButton(t.T("common_proceed", "Proceed"), func() {
		if w.onProceed != nil {
			w.onProceed()
		}
	})

	nav := container.NewHBox(w.backButton, w.homeButton, layout.NewSpacer(), w.cancelButton, w.proceedButton)
	w.content = container.NewStack()

	head := container.NewVBox(w.titleLabel, w.stepLabel, widget.NewSeparator())
	footer := container.NewVBox(widget.NewSeparator(), nav)
	return container.NewBorder(head, footer, nil, nil, w.content)
}

func (w *workflowController) Canvas() fyne.CanvasObject {
	return w.root
}

func (w *workflowController) SetStep(step int) {
	w.stepLabel.SetText(t.Td("workflow_step_indicator", "Step {{.Current}}/{{.Total}}: {{.Name}}", map[string]any{
		"Current": step + 1,
		"Total":   workflowStepCount,
		"Name":    workflowStepName(step),
	}))
}

func (w *workflowController) SetContent(content fyne.CanvasObject) {
	w.content.Objects = []fyne.CanvasObject{content}
	w.content.Refresh()
}

func (w *workflowController) SetButtons(state workflowButtonState) {
	w.setEnabled(w.backButton, state.BackEnabled)
	w.setEnabled(w.homeButton, state.HomeEnabled)
	w.setEnabled(w.cancelButton, state.CancelEnabled)
	w.setEnabled(w.proceedButton, state.ProceedEnabled)
	if state.ProceedLabel != "" {
		w.proceedButton.SetText(state.ProceedLabel)
	}
}

func (w *workflowController) setEnabled(button *widget.Button, enabled bool) {
	if button == nil {
		return
	}
	if enabled {
		button.Enable()
		return
	}
	button.Disable()
}
