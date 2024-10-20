package main

import (
	"context"
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (state *AppState) initializeUI() {
	// Initialize UI components
	state.helperText = tview.NewTextView()
	state.updateHelperText()

	state.contextDropdown = tview.NewDropDown()
	state.contextDropdown.SetLabel("Context: ")
	state.contextDropdown.SetOptions([]string{"Loading contexts..."}, nil)
	state.contextDropdown.SetDisabled(true)

	state.namespaceDropdown = tview.NewDropDown()
	state.namespaceDropdown.SetLabel("Namespace: ")
	state.namespaceDropdown.SetOptions([]string{"Loading namespaces..."}, nil)
	state.namespaceDropdown.SetDisabled(true)

	state.searchInput = tview.NewInputField()
	state.searchInput.SetLabel("Search: ")
	state.searchInput.SetFieldWidth(30)

	state.treeView = tview.NewTreeView()
	state.treeView.SetBorder(true)
	state.treeView.SetTitle("Namespaces and Pods")
	rootNode := tview.NewTreeNode("Please select a namespace to load pods").SetColor(tcell.ColorYellow)
	state.treeView.SetRoot(rootNode).SetCurrentNode(rootNode)

	state.secondSection = tview.NewTextView()
	state.secondSection.SetDynamicColors(true)
	state.secondSection.SetRegions(true)
	state.secondSection.SetTextAlign(tview.AlignLeft)
	state.secondSection.SetText("Output will be displayed here")

	state.modal = tview.NewModal()

	state.grid = tview.NewGrid()
	state.grid.SetRows(4, 1, 0)
	state.grid.SetColumns(0, 0, 0)
	state.grid.SetBorders(true)
	state.grid.AddItem(state.helperText, 0, 0, 1, 3, 0, 0, false)
	state.grid.AddItem(state.contextDropdown, 1, 0, 1, 1, 0, 0, false)
	state.grid.AddItem(state.namespaceDropdown, 1, 1, 1, 1, 0, 0, false)
	state.grid.AddItem(state.searchInput, 1, 2, 1, 1, 0, 0, false)
	state.grid.AddItem(state.treeView, 2, 0, 1, 1, 0, 0, true)
	state.grid.AddItem(state.secondSection, 2, 1, 1, 2, 0, 0, false)

	state.pages = tview.NewPages()
	state.pages.AddPage("main", state.grid, true, true)

	state.app.SetRoot(state.pages, true)
	state.setFocusHighlight(state.contextDropdown)

	// Event handlers
	state.setupEventHandlers()
}

func (state *AppState) updateHelperText() {
	prometheusStatus := "Not connected"
	if state.promDetected {
		prometheusStatus = "Connected"
	}

	state.helperText.SetText(fmt.Sprintf(
		"[::b]Podminator[::d] - Prometheus: %s\n"+
			" [yellow]'o'[-] Toggle Terminals | [yellow]'l'[-] Logs | [yellow]'t'[-] Tail Logs | [yellow]'e'[-] Exec | [yellow]'E'[-] (SHIFT+e) Exec with custom command | [yellow]'i'[-] Info | [yellow]'y'[-] YAML | [yellow]'h'[-] Metrics Graphs | [yellow]'n'[-] Namespace | [yellow]'s'[-] Search | [yellow]'r'[-] Refresh | [yellow]'spacebar'[-] Jump to bottom (Pod output) | [yellow]'q'[-] Quit \n"+
			"Pods are refreshed every 60 seconds - last timestamp: [yellow]%s[-]",
		prometheusStatus, state.lastRefreshed)).
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
}

func (state *AppState) setFocusHighlight(focusedView tview.Primitive) {
	state.app.SetFocus(focusedView)

	// Reset all borders to white
	if state.treeView != nil {
		state.treeView.SetBorderColor(tcell.ColorWhite)
	}
	if state.secondSection != nil {
		state.secondSection.SetBorderColor(tcell.ColorWhite)
	}
	if state.searchInput != nil {
		state.searchInput.SetBorderColor(tcell.ColorWhite)
	}
	if state.namespaceDropdown != nil {
		state.namespaceDropdown.SetBorderColor(tcell.ColorWhite)
	}
	if state.contextDropdown != nil {
		state.contextDropdown.SetBorderColor(tcell.ColorWhite)
	}

	// Set the border color of the focused view to blue
	if focusedView == state.treeView {
		state.treeView.SetBorderColor(tcell.ColorBlue)
	} else if focusedView == state.secondSection {
		state.secondSection.SetBorderColor(tcell.ColorBlue)
	} else if focusedView == state.searchInput {
		state.searchInput.SetBorderColor(tcell.ColorBlue)
	} else if focusedView == state.namespaceDropdown {
		state.namespaceDropdown.SetBorderColor(tcell.ColorBlue)
	} else if focusedView == state.contextDropdown {
		state.contextDropdown.SetBorderColor(tcell.ColorBlue)
	}
}

func (state *AppState) setupEventHandlers() {
	// Debounce the search input changes
	debouncedUpdate := state.debounce(func() {
		state.app.QueueUpdateDraw(func() {
			err := state.updatePodTreeView(state.searchInput.GetText())
			if err != nil {
				// Handle error
			}
		})
	}, 300*time.Millisecond)

	state.searchInput.SetChangedFunc(func(text string) {
		debouncedUpdate()
	})

	state.searchInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			err := state.updatePodTreeView(state.searchInput.GetText())
			if err != nil {
				// Handle error
			}
			state.setFocusHighlight(state.treeView)
		}
	})

	state.treeView.SetChangedFunc(func(node *tview.TreeNode) {
		state.handlePodSelection(node)
	})

	state.treeView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp, tcell.KeyDown, tcell.KeyLeft, tcell.KeyRight, tcell.KeyEnter:
			return event
		default:
			return nil
		}
	})

	state.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if state.modalActive {
			return event
		}
		switch event.Rune() {
		case 'c', 'C':
			state.setFocusHighlight(state.contextDropdown)
			return nil
		case 'n', 'N':
			state.setFocusHighlight(state.namespaceDropdown)
			return nil
		case 'o', 'O':
			state.useNewTerminal = !state.useNewTerminal
			if state.useNewTerminal {
				state.secondSection.SetText("Output now in new terminal windows")
			} else {
				state.secondSection.SetText("Output now in this window")
			}
			return nil
		case 's', 'S':
			state.setFocusHighlight(state.searchInput)
			return nil
		case 'r', 'R':
			go func() {
				err := state.updatePodTreeView(state.searchInput.GetText())
				if err != nil {
					// Handle error
				}
				state.lastRefreshed = time.Now().Format("15:04:05")
				state.app.QueueUpdateDraw(func() {
					state.updateHelperText()
				})
			}()
			return nil
		case 'q', 'Q':
			state.app.Stop()
			return nil
		}

		switch state.app.GetFocus() {
		case state.searchInput, state.namespaceDropdown, state.contextDropdown:
			return event
		}

		if state.isPodHighlighted {
			currentNode := state.treeView.GetCurrentNode()
			if currentNode != nil {
				if podMeta, ok := currentNode.GetReference().(*metav1.PartialObjectMetadata); ok {
					podName := podMeta.Name
					podNamespace := podMeta.Namespace

					pod, err := state.clientset.CoreV1().Pods(podNamespace).Get(context.TODO(), podName, metav1.GetOptions{})
					if err != nil {
						if errors.IsNotFound(err) {
							state.secondSection.SetText(fmt.Sprintf("[red]Pod '%s' in namespace '%s' not found.[-]", podName, podNamespace))
							state.isPodHighlighted = false
							state.setFocusHighlight(state.treeView)
							return nil
						}
						state.secondSection.SetText(fmt.Sprintf("[red]Error fetching pod details: %v[-]", err))
						return nil
					}
					containers := pod.Spec.Containers
					switch event.Rune() {
					case 'h':
						if state.promDetected {
							go func() {
								cpuData, memData, err := state.getPrometheusMetrics(podName, podNamespace)
								if err != nil {
									state.app.QueueUpdateDraw(func() {
										state.secondSection.SetText(fmt.Sprintf("Error fetching Prometheus metrics: %v", err))
									})
									return
								}

								// Generate graphs using ntcharts
								cpuGraph := state.plotCPUGraph(cpuData, "CPU Usage (milicores)")
								memGraph := state.plotMemoryGraph(memData, "Memory Usage (megabytes)")

								// Combine the graphs
								graphText := fmt.Sprintf("%s\n\n%s", cpuGraph, memGraph)

								state.app.QueueUpdateDraw(func() {
									state.secondSection.SetText(graphText)
									state.setFocusHighlight(state.secondSection)
								})
							}()
						} else {
							state.secondSection.SetText("Prometheus Not detected")
							state.setFocusHighlight(state.treeView)
						}
					case 'y', 'Y':
						state.runYamlCommand(podName, podNamespace)
						state.setFocusHighlight(state.secondSection)
						return nil
					case 'i', 'I':
						state.runDescribeCommand(podName, podNamespace)
						state.setFocusHighlight(state.secondSection)
						return nil
					case 'l', 'L':
						if len(containers) > 1 {
							state.showContainerSelectionModal(podName, containers, func(containerName string) {
								state.runLogsCommand(podName, podNamespace, containerName)
								state.setFocusHighlight(state.secondSection)
							})
						} else {
							state.runLogsCommand(podName, podNamespace, containers[0].Name)
							state.setFocusHighlight(state.secondSection)
						}
						return nil
					case 't', 'T':
						if len(containers) > 1 {
							state.showContainerSelectionModal(podName, containers, func(containerName string) {
								state.runTailLogsInTerminal(podName, podNamespace, containerName)
							})
						} else {
							state.runTailLogsInTerminal(podName, podNamespace, containers[0].Name)
						}
						return nil
					case 'e':
						if len(containers) > 1 {
							state.showContainerSelectionModal(podName, containers, func(containerName string) {
								state.runExecInTerminal(podName, podNamespace, containerName, "/bin/sh")
								state.setFocusHighlight(state.treeView)
							})
						} else {
							state.runExecInTerminal(podName, podNamespace, containers[0].Name, "/bin/sh")
							state.setFocusHighlight(state.treeView)
						}
						return nil
					}
				}
			}
		}

		switch event.Key() {
		case tcell.KeyRight:
			state.setFocusHighlight(state.secondSection)
			return nil
		case tcell.KeyLeft:
			state.setFocusHighlight(state.treeView)
			return nil
		}
		return event
	})

	state.secondSection.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyRune:
			if event.Rune() == ' ' {
				state.secondSection.ScrollToEnd()
				return nil
			}
		case tcell.KeyLeft:
			state.setFocusHighlight(state.treeView)
			return nil
		}
		return event
	})
}
