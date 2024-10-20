package main

import (
	"flag"
	"path/filepath"
	"sync"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/rivo/tview"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/homedir"
	metrics "k8s.io/metrics/pkg/client/clientset/versioned"
)

type AppState struct {
	useNewTerminal          bool
	selectedNamespace       string
	selectedContext         string
	namespaceOptions        []string
	contextOptions          []string
	namespaceExpansionState map[string]bool
	lastRefreshed           string
	modalActive             bool
	isPodHighlighted        bool
	kubeconfig              *string

	app               *tview.Application
	treeView          *tview.TreeView
	helperText        *tview.TextView
	searchInput       *tview.InputField
	secondSection     *tview.TextView
	namespaceDropdown *tview.DropDown
	contextDropdown   *tview.DropDown
	modal             *tview.Modal
	grid              *tview.Grid
	pages             *tview.Pages

	clientset       *kubernetes.Clientset
	dynamicClient   dynamic.Interface
	metricsClient   *metrics.Clientset
	k8sClientsReady chan struct{}

	metricsModalOpen bool

	mu sync.Mutex

	promClient    promv1.API
	promDetected  bool
	prometheusURL *string
}

func (state *AppState) initializeApp() {
	state.app = tview.NewApplication()

	if home := homedir.HomeDir(); home != "" {
		state.kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		state.kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}

	state.prometheusURL = flag.String("prometheus-url", "", "(optional) URL of the Prometheus server (e.g., http://localhost:9090)")

	flag.Parse()

	state.lastRefreshed = time.Now().Format("15:04:05")
}
