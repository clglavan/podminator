package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rivo/tview"
	v1 "k8s.io/api/core/v1"
)

func detectTerminalProgram() string {
	termProgram := os.Getenv("TERM_PROGRAM")
	if termProgram == "" {
		termProgram = "Terminal"
	}
	return termProgram
}

func runInTerminal(command string) error {
	terminalApp := detectTerminalProgram()
	var appleScript string

	switch terminalApp {
	case "iTerm.app":
		appleScript = fmt.Sprintf(`tell application "iTerm"
            create window with default profile
            tell current session of current window
                write text "bash -c '%s'"
            end tell
        end tell`, strings.ReplaceAll(command, "'", "'\\''"))
	default:
		appleScript = fmt.Sprintf(`tell application "Terminal"
            do script "bash -c '%s'"
            set bounds of front window to {100, 100, 1100, 700}
            activate
        end tell`, strings.ReplaceAll(command, "'", "'\\''"))
	}
	_, err := exec.Command("osascript", "-e", appleScript).Output()
	return err
}

func runCommandAndDisplayOutput(command string, secondSection *tview.TextView) error {
	cmd := exec.Command("bash", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	secondSection.SetText(string(output))
	return nil
}

func (state *AppState) debounce(f func(), delay time.Duration) func() {
	var timer *time.Timer
	return func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(delay, f)
	}
}

func (state *AppState) runYamlCommand(podName, podNamespace string) {
	command := fmt.Sprintf("kubectl get pod %s --namespace=%s -o yaml", podName, podNamespace)
	if state.useNewTerminal {
		err := runInTerminal(command)
		if err != nil {
			// Handle error
		}
	} else {
		err := runCommandAndDisplayOutput(command, state.secondSection)
		if err != nil {
			state.secondSection.SetText(fmt.Sprintf("Error running command: %v", err))
		}
	}
}

func (state *AppState) runDescribeCommand(podName, podNamespace string) {
	command := fmt.Sprintf("kubectl describe pod %s --namespace=%s", podName, podNamespace)
	if state.useNewTerminal {
		err := runInTerminal(command)
		if err != nil {
			// Handle error
		}
	} else {
		err := runCommandAndDisplayOutput(command, state.secondSection)
		if err != nil {
			state.secondSection.SetText(fmt.Sprintf("Error running command: %v", err))
		}
	}
}

func (state *AppState) runLogsCommand(podName, podNamespace, containerName string) {
	command := fmt.Sprintf("kubectl logs %s --namespace=%s -c %s", podName, podNamespace, containerName)
	if state.useNewTerminal {
		err := runInTerminal(command)
		if err != nil {
			// Handle error
		}
	} else {
		err := runCommandAndDisplayOutput(command, state.secondSection)
		if err != nil {
			state.secondSection.SetText(fmt.Sprintf("Error running command: %v", err))
		}
	}
}

func (state *AppState) runTailLogsInTerminal(podName, podNamespace, containerName string) {
	command := fmt.Sprintf("kubectl logs -f %s --namespace=%s -c %s", podName, podNamespace, containerName)
	err := runInTerminal(command)
	if err != nil {
		// Handle error
	}
}

func (state *AppState) runExecInTerminal(podName, podNamespace, containerName, command string) {
	fullCommand := fmt.Sprintf("kubectl exec -it %s --namespace=%s -c %s -- %s", podName, podNamespace, containerName, command)
	err := runInTerminal(fullCommand)
	if err != nil {
		// Handle error
	}
}

func (state *AppState) showContainerSelectionModal(podName string, containers []v1.Container, commandFunc func(containerName string)) {
	state.modal.ClearButtons()
	var buttons []string
	for _, container := range containers {
		buttons = append(buttons, container.Name)
	}
	buttons = append(buttons, "Cancel")
	state.modal = tview.NewModal().
		SetText(fmt.Sprintf("Select a container for pod '%s':", podName)).
		AddButtons(buttons).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel != "Cancel" {
				commandFunc(buttonLabel)
			}
			state.pages.RemovePage("containerModal")
			state.modalActive = false
			state.setFocusHighlight(state.treeView)
		})
	state.pages.AddPage("containerModal", state.modal, true, true)
	state.modalActive = true
}

func minMax(data []float64) (min, max float64) {
	if len(data) == 0 {
		return 0, 0
	}
	min = data[0]
	max = data[0]
	for _, v := range data {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max
}
