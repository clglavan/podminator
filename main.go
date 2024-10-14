package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort" // Added import for sorting
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Global variables
var (
	useNewTerminal    = false  // Toggle to switch between terminal output or displaying output in the UI
	selectedNamespace = "all"  // Default namespace selection
	namespaceOptions  []string // Stores available namespace options
	modalActive       bool     // Tracks if the modal is currently active
	lastRefreshed     string   // Stores the last refresh timestamp
	app               *tview.Application
	treeView          *tview.TreeView
	helperText        *tview.TextView
	searchInput       *tview.InputField
	secondSection     *tview.TextView
	namespaceDropdown *tview.DropDown
	modal             *tview.Modal
	clientset         *kubernetes.Clientset
	grid              *tview.Grid
	mu                sync.Mutex // Mutex to protect access to shared variables
	isPodHighlighted  bool

	// Added for preserving expansion state and selected pod
	namespaceExpansionState = make(map[string]bool)
	dynamicClient           dynamic.Interface
	k8sClientsReady         = make(chan struct{})
)

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
			// Execute the command for the selected container
			commandFunc(buttonLabel)
			// Do not reset the modal state here
		} else {
			// If no buttonLabel (e.g., user cancels), close the modal
			modalActive = false
			app.SetRoot(grid, true).SetFocus(treeView)
			setFocusHighlight(treeView)
		}
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

// Updated `runExecInTerminal` function to accept custom command
func runExecInTerminal(podName, podNamespace, containerName, command string) {
	fullCommand := fmt.Sprintf("kubectl exec -it %s --namespace=%s -c %s -- %s", podName, podNamespace, containerName, command)
	err := runInTerminal(fullCommand)
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

// Debounce function to limit the rate of function calls
func debounce(f func(), delay time.Duration) func() {
	var timer *time.Timer
	return func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(delay, f)
	}
}

// Function to record expansion states of namespace nodes
func recordExpansionState(node *tview.TreeNode) {
	// If the node is a namespace node (level 1)
	if node.GetLevel() == 1 {
		namespaceName := node.GetText()
		namespaceExpansionState[namespaceName] = node.IsExpanded()
	}

	// Recursively traverse child nodes
	for _, child := range node.GetChildren() {
		recordExpansionState(child)
	}
}

// Updated updatePodTreeView function
func updatePodTreeView(clientset *kubernetes.Clientset, selectedNamespace string, tree *tview.TreeView, app *tview.Application, searchQuery string) error {

	// Check if clients are ready
	select {
	case <-k8sClientsReady:
		// Proceed
	default:
		// Clients not ready
		return fmt.Errorf("Kubernetes clients are not initialized yet")
	}

	rootNode := tview.NewTreeNode("Namespaces").SetColor(tcell.ColorGreen)

	// Record expansion states before rebuilding the tree
	existingRoot := tree.GetRoot()
	if existingRoot != nil {
		recordExpansionState(existingRoot)
	}

	// Variables to store the selected pod's namespace and name
	var previouslySelectedPodNamespace, previouslySelectedPodName string

	// Capture the selected pod before rebuilding the tree
	currentNode := tree.GetCurrentNode()
	if currentNode != nil {
		if podMeta, ok := currentNode.GetReference().(*metav1.PartialObjectMetadata); ok {
			previouslySelectedPodNamespace = podMeta.Namespace
			previouslySelectedPodName = podMeta.Name
		}
	}

	// Build a map of namespaces with pods
	namespacesWithPods := make(map[string][]metav1.PartialObjectMetadata)

	if selectedNamespace == "all" {
		// Fetch the list of namespaces
		namespaceList, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}

		// Fetch pods from each namespace sequentially
		for _, ns := range namespaceList.Items {
			nsName := ns.Name

			podList, err := dynamicClient.Resource(schema.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "pods",
			}).Namespace(nsName).List(context.TODO(), metav1.ListOptions{
				// Specify that we want PartialObjectMetadata
				TypeMeta: metav1.TypeMeta{
					Kind:       "PartialObjectMetadataList",
					APIVersion: "meta.k8s.io/v1",
				},
			})
			if err != nil {
				// Handle the error gracefully, e.g., log and continue
				log.Printf("Error fetching pods in namespace %s: %v", nsName, err)
				continue
			}

			// Convert the UnstructuredList to PartialObjectMetadataList
			var metadataList metav1.PartialObjectMetadataList
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(podList.UnstructuredContent(), &metadataList)
			if err != nil {
				log.Printf("Error converting pods in namespace %s: %v", nsName, err)
				continue
			}

			if len(metadataList.Items) > 0 {
				namespacesWithPods[nsName] = metadataList.Items
			}
		}
	} else {
		// Fetch pods from the selected namespace
		podList, err := dynamicClient.Resource(schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "pods",
		}).Namespace(selectedNamespace).List(context.TODO(), metav1.ListOptions{
			// Specify that we want PartialObjectMetadata
			TypeMeta: metav1.TypeMeta{
				Kind:       "PartialObjectMetadataList",
				APIVersion: "meta.k8s.io/v1",
			},
		})
		if err != nil {
			return err
		}

		// Convert the UnstructuredList to PartialObjectMetadataList
		var metadataList metav1.PartialObjectMetadataList
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(podList.UnstructuredContent(), &metadataList)
		if err != nil {
			return err
		}

		if len(metadataList.Items) > 0 {
			namespacesWithPods[selectedNamespace] = metadataList.Items
		}
	}

	// If a search query is provided, filter the pods
	if searchQuery != "" {
		for nsName, podList := range namespacesWithPods {
			var matchingPods []metav1.PartialObjectMetadata
			for _, podMeta := range podList {
				if strings.Contains(strings.ToLower(podMeta.Name), strings.ToLower(searchQuery)) {
					matchingPods = append(matchingPods, podMeta)
				}
			}
			if len(matchingPods) > 0 {
				namespacesWithPods[nsName] = matchingPods
			} else {
				delete(namespacesWithPods, nsName)
			}
		}
	}

	// Collect and sort the namespace names
	var namespaceNames []string
	for nsName := range namespacesWithPods {
		namespaceNames = append(namespaceNames, nsName)
	}
	sort.Strings(namespaceNames)

	// Build the tree view nodes
	for _, nsName := range namespaceNames {
		podList := namespacesWithPods[nsName]
		nsNode := tview.NewTreeNode(nsName).SetColor(tcell.ColorYellow)

		// Set expansion state based on previous state
		if expanded, exists := namespaceExpansionState[nsName]; exists {
			nsNode.SetExpanded(expanded)
		} else {
			nsNode.SetExpanded(false) // Default to collapsed if no previous state
		}

		// Correctly capture nsName in the closure
		nsNameCopy := nsName

		// Set the function to expand/collapse the namespace node when selected
		nsNode.SetSelectedFunc(func(node *tview.TreeNode) func() {
			return func() {
				if node.IsExpanded() {
					node.SetExpanded(false)
					namespaceExpansionState[nsNameCopy] = false
				} else {
					node.SetExpanded(true)
					namespaceExpansionState[nsNameCopy] = true
				}
			}
		}(nsNode))

		// Create a "Pods" node under the namespace
		podsNode := tview.NewTreeNode("Pods").SetColor(tcell.ColorWhite)
		podsNode.SetExpanded(true) // Expand the "Pods" node by default

		// Add pods to the "Pods" node
		for _, podMeta := range podList {
			// Capture podMeta for closure
			podMetaCopy := podMeta
			podNode := tview.NewTreeNode(podMeta.Name).SetReference(&podMetaCopy).SetColor(tcell.ColorWhite)

			// Set the selected function for the pod node
			podNode.SetSelectedFunc(func() {
				// Set the current node to the selected pod node
				tree.SetCurrentNode(podNode)
				// Handle pod selection to update isPodHighlighted and UI
				handlePodSelection(podNode)
			})

			podsNode.AddChild(podNode)
		}

		// Add the "Pods" node under the namespace node
		nsNode.AddChild(podsNode)

		rootNode.AddChild(nsNode)
	}

	// If no namespaces with pods are found
	if len(rootNode.GetChildren()) == 0 {
		rootNode.AddChild(tview.NewTreeNode("No matching pods found").SetColor(tcell.ColorRed))
	}

	// Update the tree view
	tree.SetRoot(rootNode)
	tree.SetCurrentNode(rootNode)

	// Function to recursively find and return the pod node, its parent (Pods node), and the namespace node
	var findPodNode func(node *tview.TreeNode, namespace, podName string) (*tview.TreeNode, *tview.TreeNode, *tview.TreeNode)
	findPodNode = func(node *tview.TreeNode, namespace, podName string) (*tview.TreeNode, *tview.TreeNode, *tview.TreeNode) {
		if node.GetText() == namespace {
			// This is the namespace node
			for _, child := range node.GetChildren() {
				if child.GetText() == "Pods" {
					podsNode := child
					for _, podNode := range podsNode.GetChildren() {
						if podMeta, ok := podNode.GetReference().(*metav1.PartialObjectMetadata); ok {
							if podMeta.Namespace == namespace && podMeta.Name == podName {
								return podNode, podsNode, node // Return podNode, podsNode, namespaceNode
							}
						}
					}
				}
			}
		} else {
			for _, child := range node.GetChildren() {
				foundNode, podsNode, namespaceNode := findPodNode(child, namespace, podName)
				if foundNode != nil {
					return foundNode, podsNode, namespaceNode
				}
			}
		}
		return nil, nil, nil
	}

	// After rebuilding the tree, attempt to re-select the previously selected pod
	if previouslySelectedPodName != "" && previouslySelectedPodNamespace != "" {
		// Find the pod node in the new tree
		podNode, podsNode, namespaceNode := findPodNode(rootNode, previouslySelectedPodNamespace, previouslySelectedPodName)
		if podNode != nil {
			// Expand the Pods node
			if podsNode != nil {
				podsNode.SetExpanded(true)
			}
			// Expand the namespace node
			if namespaceNode != nil {
				namespaceNode.SetExpanded(true)
			}
			// Set the current node to the pod node
			tree.SetCurrentNode(podNode)
		} else {
			// Pod no longer exists; reset selection to root
			tree.SetCurrentNode(rootNode)
		}
	}

	return nil
}

// Function to handle pod selection and update the UI
func handlePodSelection(node *tview.TreeNode) {
	if podMeta, ok := node.GetReference().(*metav1.PartialObjectMetadata); ok {
		isPodHighlighted = true
		secondSection.SetText(fmt.Sprintf("Highlighted Pod: %s. Use the menu at the top to interact", podMeta.Name))
	} else {
		isPodHighlighted = false
	}
}

// Function to highlight the currently focused view by changing its border color
func setFocusHighlight(focusedView tview.Primitive) {
	app.SetFocus(focusedView)
	// Reset all borders to white
	if treeView != nil {
		treeView.SetBorderColor(tcell.ColorWhite)
	}
	if secondSection != nil {
		secondSection.SetBorderColor(tcell.ColorWhite)
	}
	if searchInput != nil {
		searchInput.SetBorderColor(tcell.ColorWhite)
	}
	if namespaceDropdown != nil {
		namespaceDropdown.SetBorderColor(tcell.ColorWhite)
	}

	// Set the border color of the focused view to blue
	switch focusedView.(type) {
	case *tview.TreeView:
		if treeView != nil {
			treeView.SetBorderColor(tcell.ColorBlue)
		}
	case *tview.TextView:
		if secondSection != nil {
			secondSection.SetBorderColor(tcell.ColorBlue)
		}
	case *tview.InputField:
		if searchInput != nil {
			searchInput.SetBorderColor(tcell.ColorBlue)
		}
	case *tview.DropDown:
		if namespaceDropdown != nil {
			namespaceDropdown.SetBorderColor(tcell.ColorBlue)
		}
	}
}

// Function to update the helper text with the last refresh timestamp
func updateHelperText(helperText *tview.TextView) {
	helperText.SetText(fmt.Sprintf("[::b]Podminator[::d]\n [yellow]'o'[-] Toggle Terminals | [yellow]'l'[-] Logs | [yellow]'t'[-] Tail Logs | [yellow]'e'[-] Exec | [yellow]'E'[-] (SHIFT+e) Exec with custom command | [yellow]'i'[-] Info | [yellow]'y'[-] YAML | [yellow]'n'[-] Namespace | [yellow]'s'[-] Search | [yellow]'r'[-] Refresh | [yellow]'spacebar'[-] Jump to bottom (Pod output) | [yellow]'q'[-] Quit \nPods are refreshed every 60 seconds - last timestamp: [yellow]%s[-]", lastRefreshed)).
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
}

// Function to periodically refresh the pod list
func periodicPodRefresh(clientset *kubernetes.Clientset, app *tview.Application, treeView *tview.TreeView, helperText *tview.TextView, searchInput *tview.InputField) {
	// Create a ticker to trigger the refresh every 60 seconds
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check if clients are ready
			select {
			case <-k8sClientsReady:
				// Proceed
			default:
				// Clients not ready, skip this iteration
				continue
			}

			// Fetch data in a separate goroutine
			go func() {
				var selectedNamespaceLocal string
				// Access UI elements within app.QueueUpdateDraw
				app.QueueUpdateDraw(func() {
					selectedOption, _ := namespaceDropdown.GetCurrentOption()
					selectedNamespaceLocal = namespaceOptions[selectedOption]
				})
				if selectedNamespaceLocal == "Select a namespace" {
					// Do not refresh if no namespace is selected
					return
				}
				searchQuery := searchInput.GetText()

				// Fetch data from Kubernetes API
				err := updatePodTreeView(clientset, selectedNamespaceLocal, treeView, app, searchQuery)
				if err != nil {
					log.Printf("Error updating tree view: %v", err)
					return
				}

				// Update the last refreshed time
				lastRefreshed = time.Now().Format("15:04:05")

				// Update UI elements on the main thread
				app.QueueUpdateDraw(func() {
					// Update helper text
					updateHelperText(helperText)
				})
			}()
		}
	}
}

func loadNamespaces() {
	mu.Lock()
	cs := clientset
	mu.Unlock()

	// Load the namespaces
	namespaceList, err := cs.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Printf("Error retrieving namespaces: %v", err)
		return
	}

	// Collect and sort the namespace names
	var namespaceNames []string
	for _, ns := range namespaceList.Items {
		namespaceNames = append(namespaceNames, ns.Name)
	}
	sort.Strings(namespaceNames)

	// Build the new namespace options
	newNamespaceOptions := append([]string{"Select a namespace", "all"}, namespaceNames...)

	// Update the UI in the main thread
	app.QueueUpdateDraw(func() {
		namespaceOptions = newNamespaceOptions
		// Update the dropdown options and enable it
		namespaceDropdown.SetOptions(namespaceOptions, namespaceSelectHandler)
		// Set the current option to "Select a namespace"
		namespaceDropdown.SetCurrentOption(0)
		// Enable the dropdown
		namespaceDropdown.SetDisabled(false)
	})
}

func namespaceSelectHandler(option string, index int) {
	selectedNamespace = option
	// Clear the search input when namespace changes
	searchInput.SetText("")
	if selectedNamespace == "Select a namespace" {
		// Clear the tree view
		rootNode := tview.NewTreeNode("Please select a namespace to load pods").SetColor(tcell.ColorYellow)
		treeView.SetRoot(rootNode).SetCurrentNode(rootNode)
		secondSection.SetText("Output will be displayed here")
	} else {
		// Update the tree view when a new namespace is selected
		err := updatePodTreeView(clientset, selectedNamespace, treeView, app, "")
		if err != nil {
			log.Printf("Error updating tree view: %v", err)
		}
		treeView.SetCurrentNode(treeView.GetRoot()) // Reset focus to root
		setFocusHighlight(treeView)
		if selectedNamespace == "all" {
			secondSection.SetText("[red]Warning:[-] Displaying all namespaces may affect performance.")
		} else {
			secondSection.SetText("Output will be displayed here")
		}
	}
}

func main() {
	// Initialize the application
	app = tview.NewApplication()

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

	go func() {
		// Build Kubernetes clientset
		config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
		if err != nil {
			log.Printf("Error building kubeconfig: %v", err)
			return
		}

		cs, err := kubernetes.NewForConfig(config)
		if err != nil {
			log.Printf("Error creating Kubernetes client: %v", err)
			return
		}

		// Initialize the dynamic client
		dc, err := dynamic.NewForConfig(config)
		if err != nil {
			log.Printf("Error creating dynamic client: %v", err)
			return
		}

		// Update the global variables safely
		mu.Lock()
		clientset = cs
		dynamicClient = dc
		mu.Unlock()

		// Now that clients are initialized, load namespaces
		loadNamespaces()

		// Signal that clients are ready
		close(k8sClientsReady)
	}()
	// // Build Kubernetes clientset
	// config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	// if err != nil {
	// 	log.Fatalf("Error building kubeconfig: %v", err)
	// }

	// clientset, err = kubernetes.NewForConfig(config)
	// if err != nil {
	// 	log.Fatalf("Error creating Kubernetes client: %v", err)
	// }

	// // Initialize the dynamic client
	// dynamicClient, err = dynamic.NewForConfig(config)
	// if err != nil {
	// 	log.Fatalf("Error creating dynamic client: %v", err)
	// }

	// Retrieve the list of namespaces
	// namespaceList, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	// if err != nil {
	// 	log.Fatalf("Error retrieving namespaces: %v", err)
	// }

	// // Collect and sort the namespace names
	// var namespaceNames []string
	// for _, ns := range namespaceList.Items {
	// 	namespaceNames = append(namespaceNames, ns.Name)
	// }
	// sort.Strings(namespaceNames)

	// Populate the namespace dropdown with "all" at the top
	// Populate the namespace dropdown with "Select a namespace" at the top
	// namespaceOptions = append([]string{"Select a namespace", "all"}, namespaceNames...)

	// Sort the namespace options alphabetically
	// sort.Strings(namespaceOptions)

	// Initialize the lastRefreshed variable with the current timestamp
	lastRefreshed = time.Now().Format("15:04:05")

	// Initialize treeView before setting up namespaceDropdown
	treeView = tview.NewTreeView()
	treeView.SetBorder(true).SetTitle("Namespaces and Pods")

	// Initialize modal
	modal = tview.NewModal()

	// Helper text at the top of the UI
	helperText = tview.NewTextView()
	updateHelperText(helperText)

	// Search input for filtering the pod list
	searchInput = tview.NewInputField().
		SetLabel("Search: ").
		SetFieldWidth(30)

	// Output section to display command output (logs, describe, etc.)
	secondSection = tview.NewTextView().
		SetTextAlign(tview.AlignLeft).
		SetScrollable(true).
		SetText("Output will be displayed here")

	// Bind the "space" key to scroll to the bottom in the secondSection
	secondSection.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyRune:
			if event.Rune() == ' ' { // Space key
				secondSection.ScrollToEnd() // Scroll to the bottom
				return nil
			}
		case tcell.KeyLeft:
			setFocusHighlight(treeView)
			return nil
		}
		return event
	})

	// namespaceDropdown = tview.NewDropDown()
	// Now set its properties
	// Initialize the namespaceDropdown
	// namespaceDropdown.SetLabel("Namespace: ").
	// 	SetOptions(namespaceOptions, func(option string, index int) {
	// 		selectedNamespace = option
	// 		// Clear the search input when namespace changes
	// 		searchInput.SetText("")
	// 		if selectedNamespace == "Select a namespace" {
	// 			// Clear the tree view
	// 			rootNode := tview.NewTreeNode("Please select a namespace to load pods").SetColor(tcell.ColorYellow)
	// 			treeView.SetRoot(rootNode).SetCurrentNode(rootNode)
	// 			secondSection.SetText("Output will be displayed here")
	// 		} else {
	// 			// Update the tree view when a new namespace is selected
	// 			err := updatePodTreeView(clientset, selectedNamespace, treeView, app, "")
	// 			if err != nil {
	// 				log.Printf("Error updating tree view: %v", err)
	// 			}
	// 			treeView.SetCurrentNode(treeView.GetRoot()) // Reset focus to root
	// 			setFocusHighlight(treeView)
	// 			if selectedNamespace == "all" {
	// 				secondSection.SetText("[red]Warning:[-] Displaying all namespaces may affect performance.")
	// 			} else {
	// 				secondSection.SetText("Output will be displayed here")
	// 			}
	// 		}
	// 	})

	// Now that namespaceDropdown is fully initialized, set the current option
	// namespaceDropdown.SetCurrentOption(0)

	// Initialize namespaceOptions with "Loading namespaces"
	namespaceOptions = []string{"Loading namespaces"}

	// Initialize the namespaceDropdown
	namespaceDropdown = tview.NewDropDown()
	namespaceDropdown.SetLabel("Namespace: ").
		SetOptions(namespaceOptions, func(option string, index int) {
			// The dropdown is disabled, so this won't be called yet
		}).
		SetDisabled(true)

		// Start a goroutine to load namespaces
	// go func() {
	// 	// Load the namespaces
	// 	namespaceList, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	// 	if err != nil {
	// 		log.Printf("Error retrieving namespaces: %v", err)
	// 		return
	// 	}

	// 	// Collect and sort the namespace names
	// 	var namespaceNames []string
	// 	for _, ns := range namespaceList.Items {
	// 		namespaceNames = append(namespaceNames, ns.Name)
	// 	}
	// 	sort.Strings(namespaceNames)

	// 	// Build the new namespace options
	// 	newNamespaceOptions := append([]string{"Select a namespace", "all"}, namespaceNames...)

	// 	// Update the UI in the main thread
	// 	app.QueueUpdateDraw(func() {
	// 		namespaceOptions = newNamespaceOptions
	// 		// Update the dropdown options and enable it
	// 		namespaceDropdown.SetOptions(namespaceOptions, func(option string, index int) {
	// 			selectedNamespace = option
	// 			// Clear the search input when namespace changes
	// 			searchInput.SetText("")
	// 			if selectedNamespace == "Select a namespace" {
	// 				// Clear the tree view
	// 				rootNode := tview.NewTreeNode("Please select a namespace to load pods").SetColor(tcell.ColorYellow)
	// 				treeView.SetRoot(rootNode).SetCurrentNode(rootNode)
	// 				secondSection.SetText("Output will be displayed here")
	// 			} else {
	// 				// Update the tree view when a new namespace is selected
	// 				err := updatePodTreeView(clientset, selectedNamespace, treeView, app, "")
	// 				if err != nil {
	// 					log.Printf("Error updating tree view: %v", err)
	// 				}
	// 				treeView.SetCurrentNode(treeView.GetRoot()) // Reset focus to root
	// 				setFocusHighlight(treeView)
	// 				if selectedNamespace == "all" {
	// 					secondSection.SetText("[red]Warning:[-] Displaying all namespaces may affect performance.")
	// 				} else {
	// 					secondSection.SetText("Output will be displayed here")
	// 				}
	// 			}
	// 		})
	// 		// Set the current option to "Select a namespace"
	// 		namespaceDropdown.SetCurrentOption(0)
	// 		// Enable the dropdown
	// 		namespaceDropdown.SetDisabled(false)
	// 	})
	// }()

	// Create the grid layout for the UI
	grid = tview.NewGrid().
		SetRows(4, 1, 0).
		SetColumns(0, 0).
		SetBorders(true).
		AddItem(helperText, 0, 0, 1, 2, 0, 0, false).
		AddItem(searchInput, 1, 0, 1, 1, 0, 0, false).
		AddItem(namespaceDropdown, 1, 1, 1, 1, 0, 0, false)

	// Initialize the tree view
	// err = updatePodTreeView(clientset, selectedNamespace, treeView, app, "")
	// if err != nil {
	// 	log.Fatalf("Error updating tree view: %v", err)
	// }
	rootNode := tview.NewTreeNode("Please select a namespace to load pods").SetColor(tcell.ColorYellow)
	treeView.SetRoot(rootNode).SetCurrentNode(rootNode)

	// Add the tree view and second section to the grid
	grid.AddItem(treeView, 2, 0, 1, 1, 0, 0, true).
		AddItem(secondSection, 2, 1, 1, 1, 0, 0, false)

	// Debounce the search input changes
	debouncedUpdate := debounce(func() {
		app.QueueUpdateDraw(func() {
			err := updatePodTreeView(clientset, selectedNamespace, treeView, app, searchInput.GetText())
			if err != nil {
				log.Printf("Error updating tree view: %v", err)
			}
		})
	}, 300*time.Millisecond)

	// Update the tree view when search input changes
	searchInput.SetChangedFunc(func(text string) {
		debouncedUpdate()
	})

	// Modify the SetDoneFunc for search input
	searchInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			// Update the tree view when Enter is pressed
			err := updatePodTreeView(clientset, selectedNamespace, treeView, app, searchInput.GetText())
			if err != nil {
				log.Printf("Error updating tree view: %v", err)
			}
			// Move focus back to the tree view
			setFocusHighlight(treeView)
		}
	})

	treeView.SetChangedFunc(func(node *tview.TreeNode) {
		handlePodSelection(node)
	})

	treeView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		// Allow treeView to handle navigation keys
		case tcell.KeyUp, tcell.KeyDown, tcell.KeyLeft, tcell.KeyRight, tcell.KeyEnter:
			return event
		default:
			// For other keys, return nil to prevent treeView from handling them
			return nil
		}
	})

	// Global input handling for arrow navigation and terminal toggling
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// If modal is active, let it handle the input
		if modalActive {
			return event
		}

		// If focus is on an input field or dropdown, pass the event through
		switch app.GetFocus() {
		case searchInput, namespaceDropdown:
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
			setFocusHighlight(searchInput)
			return nil
		case 'r', 'R':
			// Check if clients are ready
			select {
			case <-k8sClientsReady:
				// Proceed
			default:
				// Clients not ready
				secondSection.SetText("Kubernetes clients are not initialized yet.")
				return nil
			}
			// Manual refresh
			go func() {
				err := updatePodTreeView(clientset, selectedNamespace, treeView, app, searchInput.GetText())
				if err != nil {
					log.Printf("Error updating tree view: %v", err)
				}
				lastRefreshed = time.Now().Format("15:04:05")
				app.QueueUpdateDraw(func() {
					updateHelperText(helperText)
				})
			}()
			return nil
		case 'q', 'Q':
			app.Stop()
		}

		// If a pod is highlighted, handle pod-specific key bindings
		if isPodHighlighted {
			// Check if clients are ready
			select {
			case <-k8sClientsReady:
				// Proceed
			default:
				// Clients not ready
				secondSection.SetText("Kubernetes clients are not initialized yet.")
				return nil
			}
			// Get the currently highlighted node
			currentNode := treeView.GetCurrentNode()
			if currentNode != nil {
				if podMeta, ok := currentNode.GetReference().(*metav1.PartialObjectMetadata); ok {
					podName := podMeta.Name
					podNamespace := podMeta.Namespace

					// Fetch the full pod object
					pod, err := clientset.CoreV1().Pods(podNamespace).Get(context.TODO(), podName, metav1.GetOptions{})
					if err != nil {
						secondSection.SetText(fmt.Sprintf("Error fetching pod details: %v", err))
						return nil
					}
					containers := pod.Spec.Containers

					// Pod-specific key bindings
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
								runTailLogsInTerminal(podName, podNamespace, containerName)
							}, grid)
						} else {
							runTailLogsInTerminal(podName, podNamespace, containers[0].Name)
						}
						return nil
					case 'e':
						if len(containers) > 1 {
							showContainerSelectionModal(app, podName, containers, func(containerName string) {
								runExecInTerminal(podName, podNamespace, containerName, "/bin/sh")
								modalActive = false
								app.SetRoot(grid, true).SetFocus(treeView)
								setFocusHighlight(treeView)
							}, grid)
						} else {
							runExecInTerminal(podName, podNamespace, containers[0].Name, "/bin/sh")
							app.SetFocus(treeView)
							setFocusHighlight(treeView)
						}
						return nil
					case 'E':
						if len(containers) > 1 {
							showContainerSelectionModal(app, podName, containers, func(containerName string) {
								showExecCommandModal(app, podName, podNamespace, containerName, grid)
							}, grid)
						} else {
							showExecCommandModal(app, podName, podNamespace, containers[0].Name, grid)
						}
						return nil
					}
				}
			}
		}

		// Arrow key navigation between treeView and secondSection
		switch event.Key() {
		case tcell.KeyRight:
			setFocusHighlight(secondSection)
			return nil
		case tcell.KeyLeft:
			setFocusHighlight(treeView)
			return nil
		}
		return event
	})

	// Launch the pod refresh function in the background
	go periodicPodRefresh(clientset, app, treeView, helperText, searchInput)

	// Run the application
	if err := app.SetRoot(grid, true).Run(); err != nil {
		panic(err)
	}
}

// Function to show the exec modal with a default command input field
func showExecCommandModal(app *tview.Application, podName string, podNamespace string, containerName string, grid *tview.Grid) {
	// Set modalActive to true to track that modal is open
	modalActive = true

	// Instructional text to explain the modal functionality and navigation
	instructionText := tview.NewTextView().
		SetText("Enter the command to execute in the pod. Use TAB to switch between fields.").
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true).
		SetTextColor(tcell.ColorYellow)

	// Create a new InputField for the exec command with a default value of "/bin/sh"
	commandInput := tview.NewInputField().
		SetLabel("Command: ").
		SetText("/bin/sh").
		SetFieldWidth(30) // Set a reasonable width for the input field

	// Create a form and add the InputField and buttons (Run and Cancel)
	form := tview.NewForm().
		AddFormItem(commandInput). // Add the input field to the form
		AddButton("Run", func() {
			// Get the command entered by the user
			command := commandInput.GetText()

			// If no command was entered, use the default "/bin/sh"
			if command == "" {
				command = "/bin/sh"
			}

			// Run the exec command with the user-specified command
			runExecInTerminal(podName, podNamespace, containerName, command)

			// Close the modal and return focus to the main grid
			modalActive = false
			app.SetRoot(grid, true).SetFocus(treeView)
			setFocusHighlight(treeView)
		}).
		AddButton("Cancel", func() {
			// Close the modal and return focus to the main grid when Cancel is pressed
			modalActive = false
			app.SetRoot(grid, true).SetFocus(treeView)
			setFocusHighlight(treeView)
		})

	// Create a centered Flex layout to hold the form and instructional text
	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).             // Add empty space at the top (flexible)
		AddItem(instructionText, 3, 1, false). // Add instructional text
		AddItem(
			tview.NewFlex().SetDirection(tview.FlexColumn).
				AddItem(nil, 0, 1, false).  // Add empty space to the left
				AddItem(form, 40, 1, true). // Center form horizontally (fixed width of 40)
				AddItem(nil, 0, 1, false),  // Add empty space to the right
						0, 1, true). // Increase vertical space to ensure input is visible
		AddItem(nil, 0, 1, false) // Add empty space at the bottom (flexible)

	// Ensure that the form is navigable with arrow keys or tab and that buttons are clickable
	form.SetCancelFunc(func() {
		// Close the modal and return focus to the main grid when Escape is pressed
		modalActive = false
		app.SetRoot(grid, true).SetFocus(treeView)
		setFocusHighlight(treeView)
	})

	// Set the focus on the command input field initially
	app.SetRoot(flex, true).SetFocus(commandInput)
}
