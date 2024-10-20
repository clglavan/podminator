package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/guptarohit/asciigraph"
	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

func (state *AppState) detectPrometheus() {
	if *state.prometheusURL != "" {
		// Use the provided Prometheus URL
		client, err := api.NewClient(api.Config{
			Address: *state.prometheusURL,
		})
		if err != nil {
			state.promDetected = false
			// fmt.Printf("Error creating Prometheus client with provided URL: %v\n", err)
			return
		}

		state.promClient = promv1.NewAPI(client)
		state.promDetected = true
		// fmt.Printf("Using provided Prometheus URL: %s\n", *state.prometheusURL)
		return
	} else {
		state.promDetected = false
	}
}

func (state *AppState) getPrometheusMetrics(podName, podNamespace string) (cpuData []float64, memData []float64, err error) {
	if !state.promDetected || state.promClient == nil {
		err = fmt.Errorf("Prometheus is not detected or not accessible")
		return
	}

	// Define the time range for the last hour
	end := time.Now()                // Current time
	start := end.Add(-1 * time.Hour) // One hour before the current time
	step := time.Minute              // Fetch data every minute

	// PromQL queries
	cpuQuery := fmt.Sprintf(`rate(container_cpu_usage_seconds_total{pod="%s",namespace="%s"}[5m])`, podName, podNamespace)
	memQuery := fmt.Sprintf(`container_memory_usage_bytes{pod="%s",namespace="%s"}`, podName, podNamespace)

	// Query CPU metrics
	cpuResult, warnings, err := state.promClient.QueryRange(context.TODO(), cpuQuery, promv1.Range{
		Start: start,
		End:   end,
		Step:  step,
	})
	if err != nil {
		return
	}
	if len(warnings) > 0 {
		fmt.Println("Warnings:", warnings)
	}
	cpuMatrix, ok := cpuResult.(model.Matrix)
	if !ok {
		err = fmt.Errorf("CPU result is not a matrix")
		return
	}

	// Process CPU data
	for _, stream := range cpuMatrix {
		for _, val := range stream.Values {
			cpuData = append(cpuData, float64(val.Value))
		}
	}

	// Query Memory metrics
	memResult, warnings, err := state.promClient.QueryRange(context.TODO(), memQuery, promv1.Range{
		Start: start,
		End:   end,
		Step:  step,
	})
	if err != nil {
		return
	}
	if len(warnings) > 0 {
		fmt.Println("Warnings:", warnings)
	}
	memMatrix, ok := memResult.(model.Matrix)
	if !ok {
		err = fmt.Errorf("Memory result is not a matrix")
		return
	}

	// Process Memory data
	for _, stream := range memMatrix {
		for _, val := range stream.Values {
			// Convert bytes to megabytes
			memData = append(memData, float64(val.Value)/(1024*1024))
		}
	}

	return
}

func plotMemoryGraph(data []float64, caption string) string {
	if len(data) == 0 {
		return "No data available to plot."
	}

	// Find min and max values to scale the Y-axis
	min, max := minMax(data)
	yLabels := []string{}

	// Determine the Y-axis step based on the height
	height := 10 // same as the height used in asciigraph.Plot
	step := (max - min) / float64(height)

	// Generate formatted Y-axis labels
	maxLabelWidth := 0
	for i := 0; i <= height; i++ {
		value := max - step*float64(i)
		label := formatBytes(int64(value))
		yLabels = append(yLabels, label)
		if len(label) > maxLabelWidth {
			maxLabelWidth = len(label)
		}
	}

	// Generate the graph without captions to simplify processing
	graph := asciigraph.Plot(data, asciigraph.Height(height), asciigraph.Width(80))

	// Split the graph into lines
	lines := strings.Split(graph, "\n")

	// Replace the Y-axis labels with formatted labels
	for i, line := range lines {
		if len(yLabels) > i {
			idx := strings.Index(line, "|")
			if idx != -1 {
				// Right-align the label based on the maximum width
				label := fmt.Sprintf("%*s", maxLabelWidth, yLabels[i])
				lines[i] = label + " " + line[idx:]
			}
		}
	}

	// Add the caption at the top
	lines = append([]string{caption}, lines...)

	// Recombine the lines
	return strings.Join(lines, "\n")
}

func plotCPUGraph(data []float64, caption string) string {
	if len(data) == 0 {
		return "No data available to plot."
	}

	// Multiply CPU usage by 1000 to convert to millicores
	for i := range data {
		data[i] = data[i] * 1000
	}

	// Find min and max values
	min, max := minMax(data)
	yLabels := []string{}

	// Determine the Y-axis step
	height := 10
	step := (max - min) / float64(height)

	// Generate formatted Y-axis labels
	maxLabelWidth := 0
	for i := 0; i <= height; i++ {
		value := max - step*float64(i)
		label := fmt.Sprintf("%.0f mC", value) // millicores
		yLabels = append(yLabels, label)
		if len(label) > maxLabelWidth {
			maxLabelWidth = len(label)
		}
	}

	// Generate the graph
	graph := asciigraph.Plot(data, asciigraph.Height(height), asciigraph.Width(80))

	// Replace Y-axis labels
	lines := strings.Split(graph, "\n")
	for i, line := range lines {
		if len(yLabels) > i {
			idx := strings.Index(line, "|")
			if idx != -1 {
				label := fmt.Sprintf("%*s", maxLabelWidth, yLabels[i])
				lines[i] = label + " " + line[idx:]
			}
		}
	}

	// Add the caption
	lines = append([]string{caption}, lines...)

	return strings.Join(lines, "\n")
}

// Padding function to ensure the graph extends to the current time
func padDataToCurrentTime(data []float64, step time.Duration, dataPointsFetched int) []float64 {
	if len(data) == 0 {
		return data
	}

	// Calculate how much time we covered with the fetched data
	totalDuration := time.Duration(dataPointsFetched) * step
	now := time.Now()
	lastDataPointTime := now.Add(-totalDuration) // Time corresponding to the first data point

	// Pad data with the last value until we reach the current time
	for lastDataPointTime.Before(now) {
		data = append(data, data[len(data)-1]) // Repeat the last value
		lastDataPointTime = lastDataPointTime.Add(step)
	}

	return data
}
