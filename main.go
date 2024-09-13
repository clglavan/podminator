package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

var useNewTerminal = false    // Toggle to switch between terminal output or displaying output in the UI
var selectedNamespace = "all" // Default namespace selection
var namespaceOptions []string // Stores available namespace options
var modalActive bool = false  // Tracks if the modal is currently active

// Function to detect which terminal application is being used (macOS-specific)
func detectTerminalProgram() string {
	termProgram := os.Getenv("TERM_PROGRAM")
	if termProgram == "" {
		termProgram = "Terminal" // Fallback to default macOS Terminal
	}
	return termProgram
}

// Function to execute a command in a new terminal window using AppleScript
func runInTerminal(command string) error {
	terminalApp := detectTerminalProgram()
	var appleScript string

	switch terminalApp {
	case "iTerm.app":
		// AppleScript for iTerm2
		appleScript = fmt.Sprintf(`tell application "iTerm"
		create window with default profile
		tell current session of current window
			write text "bash -c '%s'"
		end tell
	end tell`, strings.ReplaceAll(command, "'", "'\\''"))
	default:
		// Default AppleScript for macOS Terminal
		appleScript = fmt.Sprintf(`tell application "Terminal"
		do script "bash -c '%s'"
		set bounds of front window to {100, 100, 1100, 700}
		activate
	end tell`, strings.ReplaceAll(command, "'", "'\\''"))
	}

	// Run the AppleScript using `osascript`
	_, err := exec.Command("osascript", "-e", appleScript).Output()
	return err
}

// Function to run a command and display its output in the second section of the UI
func runCommandAndDisplayOutput(command string, secondSection *tview.TextView) error {
	cmd := exec.Command("bash", "-c", command)
	output, err := cmd.CombinedOutput() // Capture both stdout and stderr
	if err != nil {
		return err
	}
	secondSection.SetText(string(output)) // Display the output in the text view
	return nil
}

// Function to show a modal for selecting a container when a pod has multiple containers
func showContainerSelectionModal(app *tview.Application, podName string, containers []v1.Container, commandFunc func(containerName string), grid *tview.Grid) {
	modal.ClearButtons() // Clear any previous buttons from the modal

	// Add container options as buttons
	for _, container := range containers {
		containerName := container.Name
		modal.AddButtons([]string{containerName})
	}

	// Mark that the modal is active
	modalActive = true

	// Handle the "Done" function of the modal when a container is selected
	modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		if buttonLabel != "" {
			commandFunc(buttonLabel) // Execute the command for the selected container
		}

		// Restore focus to the main grid and reset the modal state
		modalActive = false
		app.SetRoot(grid, true).SetFocus(podList)
		setFocusHighlight(podList)
	})

	// Display the modal and focus on it
	app.SetRoot(modal, true).SetFocus(modal)
}

// Functions for tailing logs, executing commands, and retrieving logs
func runTailLogsInTerminal(podName, podNamespace, containerName string) {
	command := fmt.Sprintf("kubectl logs -f %s --namespace=%s -c %s", podName, podNamespace, containerName)
	err := runInTerminal(command)
	if err != nil {
		log.Printf("Error running command in terminal: %v", err)
	}
}

func runExecInTerminal(podName, podNamespace, containerName string) {
	command := fmt.Sprintf("kubectl exec -it %s --namespace=%s -c %s -- /bin/sh", podName, podNamespace, containerName)
	err := runInTerminal(command)
	if err != nil {
		log.Printf("Error running command in terminal: %v", err)
	}
}

func runLogsCommand(podName, podNamespace, containerName string, secondSection *tview.TextView) {
	command := fmt.Sprintf("kubectl logs %s --namespace=%s -c %s", podName, podNamespace, containerName)
	if useNewTerminal {
		// Run the command in a new terminal
		err := runInTerminal(command)
		if err != nil {
			log.Printf("Error running command in terminal: %v", err)
		}
	} else {
		// Display the command output in the UI
		err := runCommandAndDisplayOutput(command, secondSection)
		if err != nil {
			secondSection.SetText(fmt.Sprintf("Error running command: %v", err))
		}
	}
}

// Function to run `kubectl get pod` in YAML format
func runYamlCommand(podName, podNamespace string, secondSection *tview.TextView) {
	command := fmt.Sprintf("kubectl get pod %s --namespace=%s -o yaml", podName, podNamespace)
	if useNewTerminal {
		err := runInTerminal(command)
		if err != nil {
			log.Printf("Error running command in terminal: %v", err)
		}
	} else {
		err := runCommandAndDisplayOutput(command, secondSection)
		if err != nil {
			secondSection.SetText(fmt.Sprintf("Error running command: %v", err))
		}
	}
}

// Function to run `kubectl describe pod`
func runDescribeCommand(podName, podNamespace string, secondSection *tview.TextView) {
	command := fmt.Sprintf("kubectl describe pod %s --namespace=%s", podName, podNamespace)
	if useNewTerminal {
		err := runInTerminal(command)
		if err != nil {
			log.Printf("Error running command in terminal: %v", err)
		}
	} else {
		err := runCommandAndDisplayOutput(command, secondSection)
		if err != nil {
			secondSection.SetText(fmt.Sprintf("Error running command: %v", err))
		}
	}
}

// Function to update the pod list from the Kubernetes clientset
func updatePodList(clientset *kubernetes.Clientset, selectedNamespace string) ([]v1.Pod, error) {
	if selectedNamespace == "all" {
		pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return pods.Items, nil
	}
	pods, err := clientset.CoreV1().Pods(selectedNamespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return pods.Items, nil
}

// Function to update the UI pod list based on the search query
func updateList(searchQuery string, pods []v1.Pod, list *tview.List, app *tview.Application) {
	list.Clear()
	for _, pod := range pods {
		if strings.Contains(strings.ToLower(pod.Name), strings.ToLower(searchQuery)) {
			list.AddItem(pod.Name, "", 0, nil)
		}
	}
	list.AddItem("Quit", "Press to exit", 'q', func() {
		app.Stop()
	})
}

// Create the application
var app = tview.NewApplication()

// Create a list for displaying pods
var podList = tview.NewList()

// Various UI elements (text view, input field, dropdown, modal)
var helperText = tview.NewTextView()
var searchInput = tview.NewInputField()
var secondSection = tview.NewTextView()
var namespaceDropdown = tview.NewDropDown()
var modal = tview.NewModal()

// Function to highlight the currently focused view by changing its border color
func setFocusHighlight(focusedView tview.Primitive) {
	app.SetFocus(focusedView)
	// Reset all borders to white
	podList.SetBorderColor(tcell.ColorWhite)
	secondSection.SetBorderColor(tcell.ColorWhite)
	searchInput.SetBorderColor(tcell.ColorWhite)
	namespaceDropdown.SetBorderColor(tcell.ColorWhite)

	// Set the border color of the focused view to blue
	switch focusedView.(type) {
	case *tview.List:
		podList.SetBorderColor(tcell.ColorBlue)
	case *tview.TextView:
		secondSection.SetBorderColor(tcell.ColorBlue)
	case *tview.InputField:
		searchInput.SetBorderColor(tcell.ColorBlue)
	case *tview.DropDown:
		namespaceDropdown.SetBorderColor(tcell.ColorBlue)
	}
}

func main() {
	// Set up Kubernetes client configuration
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}

	var namespace string
	flag.StringVar(&namespace, "namespace", "", "(optional) namespace to list pods. If empty, all namespaces are considered.")
	flag.Parse()

	// Build Kubernetes clientset
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating Kubernetes client: %v", err)
	}

	// Retrieve the list of namespaces
	namespaceList, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Fatalf("Error retrieving namespaces: %v", err)
	}

	// Populate the namespace dropdown
	namespaceOptions = append(namespaceOptions, "all")
	for _, ns := range namespaceList.Items {
		namespaceOptions = append(namespaceOptions, ns.Name)
	}

	// Fetch the initial list of pods
	allPods, err := updatePodList(clientset, selectedNamespace)
	if err != nil {
		log.Fatalf("Error retrieving pods: %v", err)
	}

	// Helper text at the top of the UI
	helperText.
		SetText("[::b]Podminator[::d]\n 'o' Toggle Terminals | 'l' Logs | 't' Tail Logs | 'e' Exec | 'i' Info | 'y' YAML | 'n' Namespace | 's' Search | 'q' Quit").
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	// Search input for filtering the pod list
	searchInput.
		SetLabel("Search: ").
		SetFieldWidth(30)

	// Output section to display command output (logs, describe, etc.)
	secondSection.
		SetTextAlign(tview.AlignLeft).
		SetScrollable(true).
		SetText("Output will be displayed here")

	// Dropdown for selecting namespaces
	namespaceDropdown.
		SetLabel("Namespace: ").
		SetOptions(namespaceOptions, func(option string, index int) {
			selectedNamespace = option
			// Update the pod list when a new namespace is selected
			allPods, err = updatePodList(clientset, selectedNamespace)
			if err != nil {
				log.Fatalf("Error updating pod list: %v", err)
			}
			updateList("", allPods, podList, app)
			setFocusHighlight(podList)
		}).
		SetCurrentOption(0)

	// Create the grid layout for the UI
	grid := tview.NewGrid().
		SetRows(3, 1, 0).
		SetColumns(0, 0).
		SetBorders(true).
		AddItem(helperText, 0, 0, 1, 2, 0, 0, false).
		AddItem(searchInput, 1, 0, 1, 1, 0, 0, false).
		AddItem(namespaceDropdown, 1, 1, 1, 1, 0, 0, false).
		AddItem(podList, 2, 0, 1, 1, 0, 0, true).
		AddItem(secondSection, 2, 1, 1, 1, 0, 0, false)

	// Update the list when search input changes
	searchInput.SetChangedFunc(func(text string) {
		updateList(text, allPods, podList, app)
	})

	// Set input capture for key events in the search input field
	searchInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			// Move focus back to the pod list when Enter is pressed
			setFocusHighlight(podList)
		}
	})

	// Handle key events for pod list interactions (logs, exec, etc.)
	podList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		currentItem := podList.GetCurrentItem()
		if currentItem == -1 {
			return event
		}
		podName, _ := podList.GetItemText(currentItem)
		var podNamespace string
		var containers []v1.Container
		for _, pod := range allPods {
			if pod.Name == podName {
				podNamespace = pod.Namespace
				containers = pod.Spec.Containers
				break
			}
		}

		// Key bindings for different actions (logs, exec, yaml, etc.)
		switch event.Rune() {
		case 'y', 'Y':
			runYamlCommand(podName, podNamespace, secondSection)
			setFocusHighlight(secondSection)
			return nil
		case 'i', 'I':
			runDescribeCommand(podName, podNamespace, secondSection)
			setFocusHighlight(secondSection)
			return nil
		case 'l', 'L':
			if len(containers) > 1 {
				showContainerSelectionModal(app, podName, containers, func(containerName string) {
					runLogsCommand(podName, podNamespace, containerName, secondSection)
					setFocusHighlight(secondSection)
				}, grid)
			} else {
				runLogsCommand(podName, podNamespace, containers[0].Name, secondSection)
				setFocusHighlight(secondSection)
			}
			return nil
		case 't', 'T':
			if len(containers) > 1 {
				showContainerSelectionModal(app, podName, containers, func(containerName string) {
					runTailLogsInTerminal(podName, podNamespace, containerName) // Open tail logs in new terminal
				}, grid)
			} else {
				runTailLogsInTerminal(podName, podNamespace, containers[0].Name)
			}
			return nil
		case 'e', 'E':
			if len(containers) > 1 {
				showContainerSelectionModal(app, podName, containers, func(containerName string) {
					runExecInTerminal(podName, podNamespace, containerName) // Open exec in new terminal
				}, grid)
			} else {
				runExecInTerminal(podName, podNamespace, containers[0].Name) // Open exec in new terminal
			}
			return nil
		}

		return event
	})

	// Global input handling for arrow navigation and terminal toggling
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// If modal is active, let it handle the input
		if modalActive {
			return event
		}

		// Key bindings for global actions
		switch event.Rune() {
		case 'n', 'N':
			setFocusHighlight(namespaceDropdown)
			return nil
		case 'o', 'O':
			useNewTerminal = !useNewTerminal
			if useNewTerminal {
				secondSection.SetText("Output now in new terminal windows")
			} else {
				secondSection.SetText("Output now in this window")
			}
			return nil
		case 's', 'S':
			if app.GetFocus() != searchInput {
				setFocusHighlight(searchInput)
				return nil
			}
		}

		// Arrow key navigation between podList and secondSection
		switch event.Key() {
		case tcell.KeyRight:
			setFocusHighlight(secondSection)
			return nil
		case tcell.KeyLeft:
			setFocusHighlight(podList)
			return nil
		}
		return event
	})

	// Run the application
	if err := app.SetRoot(grid, true).Run(); err != nil {
		panic(err)
	}
}
