package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	metrics "k8s.io/metrics/pkg/client/clientset/versioned"
)

func (state *AppState) loadContexts() {
	go func() {
		kubeconfigPath := *state.kubeconfig
		config, err := clientcmd.LoadFromFile(kubeconfigPath)
		if err != nil {
			// Handle error
			return
		}

		var contexts []string
		for contextName := range config.Contexts {
			contexts = append(contexts, contextName)
		}
		sort.Strings(contexts)
		state.selectedContext = config.CurrentContext

		state.app.QueueUpdateDraw(func() {
			state.contextOptions = contexts
			state.contextDropdown.SetOptions(contexts, state.contextSelectHandler)
			state.contextDropdown.SetCurrentOption(state.getIndexOfCurrentContext(contexts, state.selectedContext))
			state.contextDropdown.SetDisabled(false)
		})

		// Initialize Kubernetes clients
		configOverrides := &clientcmd.ConfigOverrides{CurrentContext: state.selectedContext}
		clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
			configOverrides,
		)
		restConfig, err := clientConfig.ClientConfig()
		if err != nil {
			// Handle error
			return
		}

		cs, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			// Handle error
			return
		}

		dc, err := dynamic.NewForConfig(restConfig)
		if err != nil {
			// Handle error
			return
		}

		mc, err := metrics.NewForConfig(restConfig)
		if err != nil {
			// Handle error
		}

		state.mu.Lock()
		state.clientset = cs
		state.dynamicClient = dc
		state.metricsClient = mc
		state.mu.Unlock()

		// Signal that the clients are ready
		select {
		case <-state.k8sClientsReady:
		default:
			close(state.k8sClientsReady)
		}

		// Load namespaces now that clients are ready
		state.loadNamespaces()
	}()
}

func (state *AppState) loadNamespaces() {
	state.mu.Lock()
	cs := state.clientset
	state.mu.Unlock()

	namespaceList, err := cs.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		// Handle error
		return
	}

	var namespaceNames []string
	for _, ns := range namespaceList.Items {
		namespaceNames = append(namespaceNames, ns.Name)
	}
	sort.Strings(namespaceNames)
	newNamespaceOptions := append([]string{"Select a namespace", "all"}, namespaceNames...)

	state.app.QueueUpdateDraw(func() {
		state.namespaceOptions = newNamespaceOptions
		state.namespaceDropdown.SetOptions(state.namespaceOptions, state.namespaceSelectHandler)
		state.namespaceDropdown.SetCurrentOption(0)
		state.namespaceDropdown.SetDisabled(false)
	})
}

func (state *AppState) namespaceSelectHandler(option string, index int) {
	state.selectedNamespace = option
	state.searchInput.SetText("")
	if state.selectedNamespace == "Select a namespace" {
		rootNode := tview.NewTreeNode("Please select a namespace to load pods").SetColor(tcell.ColorYellow)
		state.treeView.SetRoot(rootNode).SetCurrentNode(rootNode)
		state.secondSection.SetText("Output will be displayed here")
	} else {
		err := state.updatePodTreeView("")
		if err != nil {
			// Handle error
		}
		state.treeView.SetCurrentNode(state.treeView.GetRoot())
		state.setFocusHighlight(state.treeView)
		if state.selectedNamespace == "all" {
			state.secondSection.SetText("[red]Warning:[-] Displaying all namespaces may affect performance.")
		} else {
			state.secondSection.SetText("Output will be displayed here")
		}
	}
}

func (state *AppState) contextSelectHandler(option string, index int) {
	state.selectedContext = option
	go func() {
		rawConfig, err := clientcmd.LoadFromFile(*state.kubeconfig)
		if err != nil {
			// Handle error
			return
		}
		rawConfig.CurrentContext = state.selectedContext
		config, err := clientcmd.NewDefaultClientConfig(*rawConfig, &clientcmd.ConfigOverrides{}).ClientConfig()
		if err != nil {
			// Handle error
			return
		}
		cs, err := kubernetes.NewForConfig(config)
		if err != nil {
			// Handle error
			return
		}
		dc, err := dynamic.NewForConfig(config)
		if err != nil {
			// Handle error
			return
		}
		mc, err := metrics.NewForConfig(config)
		if err != nil {
			// Handle error
		}

		state.mu.Lock()
		state.clientset = cs
		state.dynamicClient = dc
		state.metricsClient = mc
		state.mu.Unlock()

		state.loadNamespaces()
		state.namespaceExpansionState = make(map[string]bool)
		state.app.QueueUpdateDraw(func() {
			state.namespaceDropdown.SetCurrentOption(0)
		})
	}()
}

func (state *AppState) getIndexOfCurrentContext(contexts []string, currentContext string) int {
	for i, context := range contexts {
		if context == currentContext {
			return i
		}
	}
	return 0
}

func (state *AppState) periodicPodRefresh() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			select {
			case <-state.k8sClientsReady:
			default:
				continue
			}
			go func() {
				var selectedNamespaceLocal string
				state.app.QueueUpdateDraw(func() {
					selectedOption, _ := state.namespaceDropdown.GetCurrentOption()
					selectedNamespaceLocal = state.namespaceOptions[selectedOption]
				})
				if selectedNamespaceLocal == "Select a namespace" {
					return
				}
				searchQuery := state.searchInput.GetText()
				err := state.updatePodTreeView(searchQuery)
				if err != nil {
					// Handle error
					return
				}
				state.lastRefreshed = time.Now().Format("15:04:05")
				state.app.QueueUpdateDraw(func() {
					state.updateHelperText()
				})
			}()
		}
	}
}

func (state *AppState) updatePodTreeView(searchQuery string) error {
	select {
	case <-state.k8sClientsReady:
	default:
		return fmt.Errorf("Kubernetes clients are not initialized yet")
	}

	rootNode := tview.NewTreeNode("Namespaces").SetColor(tcell.ColorGreen)
	existingRoot := state.treeView.GetRoot()
	if existingRoot != nil {
		state.recordExpansionState(existingRoot)
	}

	var previouslySelectedPodNamespace, previouslySelectedPodName string
	currentNode := state.treeView.GetCurrentNode()
	if currentNode != nil {
		if podMeta, ok := currentNode.GetReference().(*metav1.PartialObjectMetadata); ok {
			previouslySelectedPodNamespace = podMeta.Namespace
			previouslySelectedPodName = podMeta.Name
		}
	}

	namespacesWithPods, err := state.fetchNamespacesWithPods(searchQuery)
	if err != nil {
		return err
	}

	var namespaceNames []string
	for nsName := range namespacesWithPods {
		namespaceNames = append(namespaceNames, nsName)
	}
	sort.Strings(namespaceNames)

	for _, nsName := range namespaceNames {
		podList := namespacesWithPods[nsName]
		nsNode := tview.NewTreeNode(nsName).SetColor(tcell.ColorYellow)
		if expanded, exists := state.namespaceExpansionState[nsName]; exists {
			nsNode.SetExpanded(expanded)
		} else {
			nsNode.SetExpanded(false)
		}
		nsNameCopy := nsName
		nsNode.SetSelectedFunc(func(node *tview.TreeNode) func() {
			return func() {
				if node.IsExpanded() {
					node.SetExpanded(false)
					state.namespaceExpansionState[nsNameCopy] = false
				} else {
					node.SetExpanded(true)
					state.namespaceExpansionState[nsNameCopy] = true
				}
			}
		}(nsNode))

		podsNode := tview.NewTreeNode("Pods").SetColor(tcell.ColorWhite)
		podsNode.SetExpanded(true)

		for _, podMeta := range podList {
			podMetaCopy := podMeta
			podNode := tview.NewTreeNode(podMeta.Name).SetReference(&podMetaCopy).SetColor(tcell.ColorWhite)
			podNode.SetSelectedFunc(func() {
				state.treeView.SetCurrentNode(podNode)
				state.handlePodSelection(podNode)
			})
			podsNode.AddChild(podNode)
		}
		nsNode.AddChild(podsNode)
		rootNode.AddChild(nsNode)
	}

	if len(rootNode.GetChildren()) == 0 {
		rootNode.AddChild(tview.NewTreeNode("No matching pods found").SetColor(tcell.ColorRed))
	}

	state.treeView.SetRoot(rootNode)
	state.treeView.SetCurrentNode(rootNode)

	state.restorePreviousSelection(rootNode, previouslySelectedPodNamespace, previouslySelectedPodName)

	return nil
}

func (state *AppState) fetchNamespacesWithPods(searchQuery string) (map[string][]metav1.PartialObjectMetadata, error) {
	namespacesWithPods := make(map[string][]metav1.PartialObjectMetadata)

	if state.selectedNamespace == "all" {
		namespaceList, err := state.clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, ns := range namespaceList.Items {
			nsName := ns.Name
			podList, err := state.fetchPodMetadataList(nsName)
			if err != nil {
				continue
			}
			if len(podList.Items) > 0 {
				namespacesWithPods[nsName] = podList.Items
			}
		}
	} else {
		podList, err := state.fetchPodMetadataList(state.selectedNamespace)
		if err != nil {
			return nil, err
		}
		if len(podList.Items) > 0 {
			namespacesWithPods[state.selectedNamespace] = podList.Items
		}
	}

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

	return namespacesWithPods, nil
}

func (state *AppState) fetchPodMetadataList(namespace string) (*metav1.PartialObjectMetadataList, error) {
	podList, err := state.dynamicClient.Resource(schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "pods",
	}).Namespace(namespace).List(context.TODO(), metav1.ListOptions{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PartialObjectMetadataList",
			APIVersion: "meta.k8s.io/v1",
		},
	})
	if err != nil {
		return nil, err
	}
	var metadataList metav1.PartialObjectMetadataList
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(podList.UnstructuredContent(), &metadataList)
	if err != nil {
		return nil, err
	}
	return &metadataList, nil
}

func (state *AppState) recordExpansionState(node *tview.TreeNode) {
	if node.GetLevel() == 1 {
		namespaceName := node.GetText()
		state.namespaceExpansionState[namespaceName] = node.IsExpanded()
	}
	for _, child := range node.GetChildren() {
		state.recordExpansionState(child)
	}
}

func (state *AppState) restorePreviousSelection(rootNode *tview.TreeNode, namespace, podName string) {
	var findPodNode func(node *tview.TreeNode, namespace, podName string) (*tview.TreeNode, *tview.TreeNode, *tview.TreeNode)
	findPodNode = func(node *tview.TreeNode, namespace, podName string) (*tview.TreeNode, *tview.TreeNode, *tview.TreeNode) {
		if node.GetText() == namespace {
			for _, child := range node.GetChildren() {
				if child.GetText() == "Pods" {
					podsNode := child
					for _, podNode := range podsNode.GetChildren() {
						if podMeta, ok := podNode.GetReference().(*metav1.PartialObjectMetadata); ok {
							if podMeta.Namespace == namespace && podMeta.Name == podName {
								return podNode, podsNode, node
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

	if podName != "" && namespace != "" {
		podNode, podsNode, namespaceNode := findPodNode(rootNode, namespace, podName)
		if podNode != nil {
			if podsNode != nil {
				podsNode.SetExpanded(true)
			}
			if namespaceNode != nil {
				namespaceNode.SetExpanded(true)
			}
			state.treeView.SetCurrentNode(podNode)
		} else {
			state.treeView.SetCurrentNode(rootNode)
		}
	}
}

func (state *AppState) handlePodSelection(node *tview.TreeNode) {
	if podMeta, ok := node.GetReference().(*metav1.PartialObjectMetadata); ok {
		state.isPodHighlighted = true
		podName := podMeta.Name
		podNamespace := podMeta.Namespace

		metrics, err := state.getPodMetrics(podNamespace, podName)
		if err != nil {
			state.secondSection.SetText(fmt.Sprintf("Error fetching metrics for pod '%s': %v[-]", podName, err))
			return
		}

		pod, err := state.clientset.CoreV1().Pods(podNamespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				state.secondSection.SetText(fmt.Sprintf("Pod '%s' in namespace '%s' not found.[-]", podName, podNamespace))
				state.isPodHighlighted = false
				state.setFocusHighlight(state.treeView)
				return
			}
			state.secondSection.SetText(fmt.Sprintf("Error fetching pod details: %v[-]", err))
			return
		}

		formattedText := state.formatPodDetails(pod, metrics)
		state.secondSection.SetText(formattedText)
	} else {
		state.isPodHighlighted = false
		state.secondSection.SetText("No pod is highlighted.")
	}
}

func (state *AppState) getPodMetrics(namespace, podName string) (*PodMetrics, error) {
	state.mu.Lock()
	mc := state.metricsClient
	state.mu.Unlock()

	if mc == nil {
		return nil, fmt.Errorf("metrics client is not initialized")
	}

	podMetrics, err := mc.MetricsV1beta1().PodMetricses(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	var totalCPU, totalMemory int64
	for _, container := range podMetrics.Containers {
		cpuQty := container.Usage.Cpu().MilliValue()
		memQty := container.Usage.Memory().Value()
		totalCPU += cpuQty
		totalMemory += memQty
	}

	cpuUsage := fmt.Sprintf("%dm", totalCPU)
	memoryUsage := formatBytes(totalMemory)

	return &PodMetrics{
		CPU:    cpuUsage,
		Memory: memoryUsage,
	}, nil
}

type PodMetrics struct {
	CPU    string
	Memory string
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func (state *AppState) formatPodDetails(pod *v1.Pod, metrics *PodMetrics) string {
	podName := pod.Name
	podNamespace := pod.Namespace
	podPhase := string(pod.Status.Phase)
	podIP := pod.Status.PodIP
	nodeName := pod.Spec.NodeName
	startTime := pod.Status.StartTime.Format("2006-01-02 15:04:05")
	hostIP := pod.Status.HostIP

	var sb strings.Builder
	sb.WriteString("[::b]Metrics:[::-]\n")
	sb.WriteString(fmt.Sprintf("CPU Usage: [yellow]%s[-]\n", metrics.CPU))
	sb.WriteString(fmt.Sprintf("Memory Usage: [yellow]%s[-]\n\n", metrics.Memory))

	sb.WriteString("[::b]Pod Information:[::-]\n")
	sb.WriteString(fmt.Sprintf("Name: [yellow]%s[-]\n", podName))
	sb.WriteString(fmt.Sprintf("Namespace: [yellow]%s[-]\n", podNamespace))
	sb.WriteString(fmt.Sprintf("Phase: [yellow]%s[-]\n", podPhase))
	sb.WriteString(fmt.Sprintf("Pod IP: [yellow]%s[-]\n", podIP))
	sb.WriteString(fmt.Sprintf("Node: [yellow]%s[-]\n", nodeName))
	sb.WriteString(fmt.Sprintf("Host IP: [yellow]%s[-]\n", hostIP))
	sb.WriteString(fmt.Sprintf("Start Time: [yellow]%s[-]\n", startTime))

	sb.WriteString("\n[::b]Containers:[::-]\n")
	for _, container := range pod.Spec.Containers {
		containerName := container.Name
		var containerStatus string
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == containerName {
				if status.State.Running != nil {
					containerStatus = "Running"
				} else if status.State.Waiting != nil {
					containerStatus = "Waiting"
				} else if status.State.Terminated != nil {
					containerStatus = "Terminated"
				} else {
					containerStatus = "Unknown"
				}
				break
			}
		}
		sb.WriteString(fmt.Sprintf("- %s: [yellow]%s[-]\n", containerName, containerStatus))
	}

	return sb.String()
}
