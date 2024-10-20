package main

import (
	"context"
	"fmt"
	"time"

	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
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

// prometheus.go
func (state *AppState) getPrometheusMetrics(podName, podNamespace string) (cpuData []float64, memData []float64, err error) {
	if !state.promDetected || state.promClient == nil {
		err = fmt.Errorf("Prometheus is not detected or not accessible")
		return
	}

	// Define the time range for the last 8 hours
	end := time.Now()
	start := end.Add(-8 * time.Hour)
	step := time.Minute * 15 // Data points

	// PromQL queries
	cpuQuery := fmt.Sprintf(`avg (rate (container_cpu_usage_seconds_total{pod="%s",namespace="%s"}[15m]))`, podName, podNamespace)
	memQuery := fmt.Sprintf(`avg (rate (container_memory_usage_bytes{pod="%s",namespace="%s"}[15m]))`, podName, podNamespace)

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
	cpuData = make([]float64, 0)
	for _, stream := range cpuMatrix {
		for _, val := range stream.Values {
			// cpuData = append(cpuData, float64(val.Value))
			// Multiply CPU value by 1000 to convert to millicores
			cpuData = append(cpuData, float64(val.Value)*1000)
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
	memData = make([]float64, 0)
	for _, stream := range memMatrix {
		for _, val := range stream.Values {
			// Convert bytes to megabytes
			memData = append(memData, float64(val.Value)/(1024*1024))
		}
	}

	return
}

func (state *AppState) plotCPUGraph(cpuData []float64, caption string) string {
	if len(cpuData) == 0 {
		return "No data available to plot."
	}

	// Increase the height for better Y-axis resolution
	tslc := timeserieslinechart.New(80, 20) // Width: 80, Height: 20

	endTime := time.Now()
	step := time.Minute
	startTime := endTime.Add(-time.Duration(len(cpuData)-1) * step)

	tslc.XLabelFormatter = timeserieslinechart.HourTimeLabelFormatter()

	for i, value := range cpuData {
		timestamp := startTime.Add(time.Duration(i) * step)
		tslc.Push(timeserieslinechart.TimePoint{
			Time:  timestamp,
			Value: value,
		})
	}

	tslc.DrawBraille()

	// Manually add the caption
	result := fmt.Sprintf("%s\n%s", caption, tslc.View())

	return result
}

func (state *AppState) plotMemoryGraph(memData []float64, caption string) string {
	if len(memData) == 0 {
		return "No data available to plot."
	}

	// Create a new TimeSeriesLineChart with the desired width and height
	tslc := timeserieslinechart.New(80, 10) // Adjust width and height as needed

	endTime := time.Now()
	step := time.Minute
	startTime := endTime.Add(-time.Duration(len(memData)-1) * step)

	tslc.XLabelFormatter = timeserieslinechart.HourTimeLabelFormatter()

	for i, value := range memData {
		timestamp := startTime.Add(time.Duration(i) * step)
		tslc.Push(timeserieslinechart.TimePoint{
			Time:  timestamp,
			Value: value,
		})
	}

	// Draw the chart using Braille characters (or use DrawASCII() if preferred)
	tslc.DrawBraille()

	// Get the chart as a string
	chartString := tslc.View()

	// Combine the caption and the chart
	result := fmt.Sprintf("%s\n%s", caption, chartString)

	return result
}
