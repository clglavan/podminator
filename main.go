package main

import (
	"sync"
)

func main() {
	// Initialize global AppState
	appState := &AppState{
		useNewTerminal:          false,
		selectedNamespace:       "all",
		namespaceExpansionState: make(map[string]bool),
		k8sClientsReady:         make(chan struct{}),
		mu:                      sync.Mutex{},
	}

	appState.initializeApp()
	appState.detectPrometheus()
	appState.initializeUI()
	appState.loadContexts()
	go appState.periodicPodRefresh()

	if err := appState.app.Run(); err != nil {
		panic(err)
	}
}
