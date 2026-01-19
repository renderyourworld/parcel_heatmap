package benchmarks

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// BenchmarkReport contains full benchmark results
type BenchmarkReport struct {
	TestName     string            `json:"test_name"`
	Timestamp    time.Time         `json:"timestamp"`
	URL          string            `json:"url"`
	Success      bool              `json:"success"`
	PageLoad     PageLoadMetrics   `json:"page_load"`
	LayerMetrics LayerMetrics      `json:"layer_metrics"`
	Basemap      BasemapMetrics    `json:"basemap_metrics"`
	ZoomMetrics  []ZoomLevelMetric `json:"zoom_metrics"`
	Duration     float64           `json:"total_duration_ms"`
}

// PageLoadMetrics captures Navigation Timing and Web Vitals
type PageLoadMetrics struct {
	DNSMs                    float64 `json:"dns_ms"`
	TCPMs                    float64 `json:"tcp_ms"`
	TTFBMs                   float64 `json:"ttfb_ms"`
	DOMContentLoadedMs       float64 `json:"dom_content_loaded_ms"`
	FullLoadMs               float64 `json:"full_load_ms"`
	FirstContentfulPaintMs   float64 `json:"fcp_ms"`
	LargestContentfulPaintMs float64 `json:"lcp_ms"`
	CumulativeLayoutShift    float64 `json:"cls"`
	MapReadyMs               float64 `json:"map_ready_ms"` // Time until entire map is idle
}

// SingleLayerMetric holds detailed timing for one layer
type SingleLayerMetric struct {
	NetworkTimeMs float64 `json:"network_time_ms"` // Total Span (First Start -> Last End)
	AvgLatencyMs  float64 `json:"avg_latency_ms"`  // Average individual request duration
	RenderTimeMs  float64 `json:"render_time_ms"`  // ReadyTime - Network Span (approx)
	ReadyTimeMs   float64 `json:"ready_time_ms"`   // Total time from nav start until visually ready
}

// LayerMetrics captures load times for individual data sources
type LayerMetrics struct {
	Protomaps          SingleLayerMetric `json:"protomaps"`
	CountiesSimplified SingleLayerMetric `json:"counties_simplified"`
	CountiesFull       SingleLayerMetric `json:"counties_full"`
}

// BasemapMetrics tracks PMTiles request performance
type BasemapMetrics struct {
	TotalRequests     int     `json:"total_requests"`
	TotalSize         int64   `json:"total_size_bytes"`
	NetworkLoadTimeMs float64 `json:"network_load_time_ms"` // Time from first req start to last req end
	AvgDurationMs     float64 `json:"avg_duration_ms"`
	MinDurationMs     float64 `json:"min_duration_ms"`
	MaxDurationMs     float64 `json:"max_duration_ms"`
}

// ZoomLevelMetric tracks performance for a specific zoom level
type ZoomLevelMetric struct {
	ZoomLevel     int     `json:"zoom_level"`
	TileRequests  int     `json:"tile_requests"`    // Number of PARCEL tiles requested
	TotalSize     int64   `json:"total_size_bytes"` // Total size of parcel tiles
	NetworkTimeMs float64 `json:"network_time_ms"`  // Time until source is loaded
	AvgLatencyMs  float64 `json:"avg_latency_ms"`   // Average individual request duration
	RenderTimeMs  float64 `json:"render_time_ms"`   // Time from source loaded to map idle
	TotalTimeMs   float64 `json:"total_time_ms"`    // Total time to reach idle state
	JSHeapUsedMB  float64 `json:"js_heap_used_mb"`  // JavaScript heap memory used (MB)
	JSHeapTotalMB float64 `json:"js_heap_total_mb"` // JavaScript heap memory total (MB)
	JSHeapDeltaMB float64 `json:"js_heap_delta_mb"` // Memory change from previous zoom level (MB)
}

// requestTrack holds timing and category for in-flight requests
type requestTrack struct {
	StartTime time.Time
	Category  string
}

// completedRequest holds details about a finished request for analysis
type completedRequest struct {
	Category string
	Start    time.Time
	End      time.Time
	Size     int64
}

// netTracker tracks start/end times and totals for a category of requests
type netTracker struct {
	FirstStart    time.Time
	LastEnd       time.Time
	TotalRequests int
	TotalSize     int64
}

// RunLighthouseAudit runs Lighthouse and opens the HTML report.
func RunLighthouseAudit(url string, mode string) error {
	ts := time.Now().Format("2006-01-02_150405")
	outDir := "logs/benchmarks"
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}
	outPath := fmt.Sprintf("%s/lighthouse_%s.html", outDir, ts)

	// Default to desktop unless mode is explicitly "mobile"
	args := []string{
		url,
		"--output=html",
		"--output-path=" + outPath,
		"--chrome-flags=--headless",
		"--enable-error-reporting=false",
	}
	if strings.ToLower(mode) != "mobile" {
		args = append(args, "--preset=desktop")
	}

	cmd := exec.Command("lighthouse", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("Running Lighthouse for %s (%s)...\n", url, mode)
	err := cmd.Run()

	// Attempt to clean up Lighthouse temp files (Windows-specific)
	tempDir := os.TempDir()
	entries, readErr := os.ReadDir(tempDir)
	if readErr == nil {
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), "lighthouse.") {
				path := tempDir + string(os.PathSeparator) + entry.Name()
				errRm := os.RemoveAll(path)
				if errRm != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to remove temp dir %s: %v\n", path, errRm)
				}
			}
		}
	}

	// Always try to open the report, even if Lighthouse errors
	openCmd := exec.Command("cmd", "/C", "start", outPath)
	_ = openCmd.Start() // Ignore error, just try to open

	return err
}

// RunPerformanceBenchmark executes the benchmark suite
func RunPerformanceBenchmark(url string) (*BenchmarkReport, error) {
	startTime := time.Now()

	log.Println("Starting performance benchmark...")
	log.Printf("Target URL: %s", url)

	// Setup Chrome
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),    // GUI mode
		chromedp.Flag("disable-gpu", false), // Enable GPU
		chromedp.WindowSize(1920, 1080),     // Fixed viewport
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
		chromedp.Flag("disable-application-cache", true),
		chromedp.Flag("enable-precise-memory-info", true), // Required for accurate performance.memory readings
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// Timeout
	ctx, cancelTimeout := context.WithTimeout(ctx, 3*time.Minute) // Increased for zoom loop
	defer cancelTimeout()

	// Network Tracking
	var mu sync.Mutex
	requests := make(map[network.RequestID]*requestTrack)

	// History log for zoom analysis
	var requestLog []completedRequest

	// Category trackers (Global)
	trackers := make(map[string]*netTracker)
	for _, k := range []string{"protomaps", "counties-simplified", "counties-full", "parcels"} {
		trackers[k] = &netTracker{}
	}

	// Helper function to categorize and track network requests
	handleNetworkRequest := func(url string, requestID network.RequestID) {
		var category string

		urlLower := strings.ToLower(url)
		if strings.Contains(urlLower, "georgia.pmtiles") {
			category = "protomaps"
		} else if strings.Contains(urlLower, "/api/tiles/") {
			category = "parcels"
		} else if strings.Contains(urlLower, "/api/counties/simplified") {
			category = "counties-simplified"
		} else if strings.Contains(urlLower, "/api/counties/full") {
			category = "counties-full"
		}

		if category != "" {
			mu.Lock()
			now := time.Now()
			requests[requestID] = &requestTrack{StartTime: now, Category: category}

			t := trackers[category]
			if t.FirstStart.IsZero() || now.Before(t.FirstStart) {
				t.FirstStart = now
			}
			mu.Unlock()
		}
	}

	handleNetworkFinished := func(requestID network.RequestID, size int64) {
		mu.Lock()
		if req, ok := requests[requestID]; ok {
			now := time.Now()

			t := trackers[req.Category]
			if now.After(t.LastEnd) {
				t.LastEnd = now
			}
			t.TotalRequests++
			t.TotalSize += size

			requestLog = append(requestLog, completedRequest{
				Category: req.Category,
				Start:    req.StartTime,
				End:      now,
				Size:     size,
			})

			delete(requests, requestID)
		}
		mu.Unlock()
	}

	handleNetworkFailed := func(requestID network.RequestID) {
		mu.Lock()
		delete(requests, requestID)
		mu.Unlock()
	}

	// 1. Setup Listener BEFORE enabling domains
	// Using ListenTarget with SetAutoAttach flatten=true should capture all network events
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *network.EventRequestWillBeSent:
			handleNetworkRequest(ev.Request.URL, ev.RequestID)

		case *network.EventLoadingFinished:
			handleNetworkFinished(ev.RequestID, int64(ev.EncodedDataLength))

		case *network.EventLoadingFailed:
			handleNetworkFailed(ev.RequestID)
		}
	})

	// 2. Enable Domains, Auto-Attach (to see Web Workers), and disable cache
	if err := chromedp.Run(ctx,
		network.Enable(),
		runtime.Enable(),
		network.SetCacheDisabled(true),
		target.SetAutoAttach(true, false).WithFlatten(true),
	); err != nil {
		return nil, fmt.Errorf("failed to enable CDP domains: %w", err)
	}

	// -------------------------------------------------------------------------
	// PHASE 1: Initial Page Load
	// -------------------------------------------------------------------------
	log.Println("Navigating to URL...")

	// We'll extract metrics via JS evaluation
	var perfData map[string]interface{}

	// Wait for map to be fully idle and collected metrics
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible("body", chromedp.ByQuery),
		chromedp.Poll(`
			(function() {
				if (!window.map || typeof window.map.isSourceLoaded !== 'function') return false;
				
				if (!window._layerReadyTimes) window._layerReadyTimes = {};
				if (!window._sourceReadyTimes) window._sourceReadyTimes = {};

				var now = performance.now();
				var layers = ['protomaps', 'counties-simplified', 'counties-full'];
				
				layers.forEach(function(id) {
					// Source Ready: Data is loaded/parsed (roughly Network Finish)
					if (!window._sourceReadyTimes[id] && window.map.getSource(id) && window.map.isSourceLoaded(id)) {
						window._sourceReadyTimes[id] = now;
					}
					// Layer Ready: Actually visible (Idle)
					if (window._sourceReadyTimes[id] && !window._layerReadyTimes[id] && window.map.loaded()) {
						window._layerReadyTimes[id] = now;
					}
				});

				var allLayersReady = layers.every(function(id) {
					return !!window._layerReadyTimes[id] && !!window._sourceReadyTimes[id];
				});
				
				if (allLayersReady && window.map.loaded()) {
					var mapReadyTime = now;
					var t = performance.timing;
					var navStart = t.navigationStart;
					
					var metrics = {
						"dns_ms": t.domainLookupEnd - t.domainLookupStart,
						"tcp_ms": t.connectEnd - t.connectStart,
						"ttfb_ms": t.responseStart - t.requestStart,
						"dom_content_loaded_ms": t.domContentLoadedEventEnd - navStart,
						"full_load_ms": t.loadEventEnd - navStart,
						"map_ready_ms": mapReadyTime,
					};

					layers.forEach(function(id) {
						metrics["layer_" + id + "_ready_ms"] = window._layerReadyTimes[id] || 0;
						metrics["source_" + id + "_ready_ms"] = window._sourceReadyTimes[id] || 0;
					});

					// Web Vitals
					var p = performance;
					var fcp = p.getEntriesByName('first-contentful-paint')[0];
					if (fcp) metrics.fcp_ms = fcp.startTime;
	
					var lcpEntries = p.getEntriesByType('largest-contentful-paint');
					if (lcpEntries.length > 0) metrics.lcp_ms = lcpEntries[lcpEntries.length - 1].startTime;
	
					var cls = 0;
					p.getEntriesByType('layout-shift').forEach(e => {
						if (!e.hadRecentInput) cls += e.value;
					});
					metrics.cls = cls;
					
					return metrics;
				}
				return false;
			})()
		`, &perfData, chromedp.WithPollingInterval(20*time.Millisecond)),
	)
	if err != nil {
		return nil, fmt.Errorf("benchmark execution failed: %w", err)
	}

	log.Printf("Map fully rendered! captured initial metrics. Stabilizing for 2s...")
	chromedp.Run(ctx, chromedp.Sleep(2*time.Second))

	// Clear tile stats and resource timings from Phase 1 so zoom level metrics are accurate
	chromedp.Run(ctx, chromedp.Evaluate(`window._tileStats = []; performance.clearResourceTimings()`, nil))

	// -------------------------------------------------------------------------
	// PHASE 2: Zoom Level Iteration (13-16)
	// -------------------------------------------------------------------------
	log.Println("Starting Zoom Level Benchmark (13-16)...")
	var zoomMetrics []ZoomLevelMetric

	center := "[-84.3880, 33.7490]" // Downtown Atlanta

	// Track where we left off in the request log for accurate per-zoom metrics
	var requestLogIndex int
	// Track previous heap size for delta calculation
	var prevHeapUsed float64

	for z := 13; z <= 16; z++ {
		log.Printf("Benchmarking Zoom Level %d...", z)

		// 1. Get initial tile count from browser AND snapshot request log index
		var initialCount int
		err = chromedp.Run(ctx, chromedp.Evaluate("window._tileCount || 0", &initialCount))
		if err != nil {
			log.Printf("Error getting initial tile count: %v", err)
		}

		// Snapshot current request log position BEFORE triggering zoom
		mu.Lock()
		requestLogIndex = len(requestLog)
		mu.Unlock()

		// 2. Trigger Zoom and record start time in browser
		err = chromedp.Run(ctx,
			chromedp.Evaluate(fmt.Sprintf(`
				window._zoomStats = { 
					start: performance.now(),
					sourceReady: 0,
					mapReady: 0
				};
				window.map.jumpTo({center: %s, zoom: %d}); 
				true;
			`, center, z), nil),
		)
		if err != nil {
			log.Printf("Error triggering zoom: %v", err)
			continue
		}

		// 3. Short sleep
		chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond))

		// 4. Poll for completion and capture statistics from the browser's own resource timing
		var stats map[string]float64
		err = chromedp.Run(ctx,
			chromedp.Poll(fmt.Sprintf(`
				(function() {
					if (!window.map || !window._zoomStats) return false;
					if (Math.abs(window.map.getZoom() - %d) > 0.01) return false;

					var s = window._zoomStats;

					// Capture the moment source is ready (Network finish)
					if (!s.sourceReady && window.map.getSource('parcels') && window.map.isSourceLoaded('parcels')) {
						s.sourceReady = performance.now();
					}

					// Capture the moment map is fully idle (Render finish)
					if (s.sourceReady && window.map.loaded()) {
						s.mapReady = performance.now();

						// Query Resource Timing API directly for tile requests
						// responseEnd is in milliseconds since timeOrigin (same as performance.now())
						const startTime = s.start;
						const allResources = performance.getEntriesByType('resource');
						const tileEntries = allResources.filter(r =>
							r.name.includes('/api/tiles/') && r.responseEnd >= startTime
						);

						const count = tileEntries.length;
						let totalSize = 0;
						let avgLatency = 0;

						if (count > 0) {
							let totalDuration = 0;
							for (const entry of tileEntries) {
								totalSize += entry.transferSize || entry.encodedBodySize || 0;
								totalDuration += entry.duration || 0;
							}
							avgLatency = totalDuration / count;
						}

						// Clear resource timings for next zoom level
						performance.clearResourceTimings();

						// Capture memory usage
						var jsHeapUsed = 0, jsHeapTotal = 0;
						if (performance.memory) {
							jsHeapUsed = performance.memory.usedJSHeapSize / (1024 * 1024);
							jsHeapTotal = performance.memory.totalJSHeapSize / (1024 * 1024);
						}

						return {
							duration: s.mapReady - s.start,
							network: s.sourceReady - s.start,
							render: s.mapReady - s.sourceReady,
							tiles: count,
							size: totalSize,
							avgLatency: avgLatency,
							jsHeapUsed: jsHeapUsed,
							jsHeapTotal: jsHeapTotal
						};
					}
					return false;
				})()
			`, z), &stats, chromedp.WithPollingInterval(20*time.Millisecond)),
		)
		if err != nil {
			log.Printf("Error waiting for zoom %d: %v", z, err)
			continue
		}

		// 5. Catch-up sleep: Wait 1s for the last few CDP events to arrive from workers
		chromedp.Run(ctx, chromedp.Sleep(1*time.Second))

		// 6. Final Tile Count Snapshot
		var finalCount int
		err = chromedp.Run(ctx, chromedp.Evaluate("window._tileCount || 0", &finalCount))
		if err != nil {
			log.Printf("Error getting final tile count: %v", err)
		}

		// 7. Build Metric (Mix of Browser timing and Go-side data counts)
		var zm ZoomLevelMetric
		zm.ZoomLevel = z
		zm.TileRequests = finalCount - initialCount
		zm.TotalTimeMs = stats["duration"]
		zm.NetworkTimeMs = stats["network"]
		zm.RenderTimeMs = stats["render"]
		zm.JSHeapUsedMB = stats["jsHeapUsed"]
		zm.JSHeapTotalMB = stats["jsHeapTotal"]
		zm.JSHeapDeltaMB = stats["jsHeapUsed"] - prevHeapUsed
		prevHeapUsed = stats["jsHeapUsed"] // Update for next iteration

		// Use browser's Resource Timing API for avg latency
		zm.AvgLatencyMs = stats["avgLatency"]

		// Debug: log tile count from Resource Timing API vs transformRequest counter
		resourceTimingTiles := int(stats["tiles"])
		log.Printf("  Zoom %d tile counts - transformRequest: %d, ResourceTiming: %d, avgLatency: %.1fms",
			z, zm.TileRequests, resourceTimingTiles, zm.AvgLatencyMs)

		// Get total size from Go-side request log if available, otherwise estimate from browser
		mu.Lock()
		for i := requestLogIndex; i < len(requestLog); i++ {
			req := requestLog[i]
			if req.Category == "parcels" {
				zm.TotalSize += req.Size
			}
		}
		mu.Unlock()

		// If Go-side didn't capture sizes, use browser's encodedBodySize (already in stats from JS)
		if zm.TotalSize == 0 && stats["size"] > 0 {
			zm.TotalSize = int64(stats["size"])
		}

		zoomMetrics = append(zoomMetrics, zm)
		log.Printf("Zoom %d done: %d tiles, %.2f KB total, %.0fms (Net: %.0fms, Lat: %.1fms) | Heap: %.1f MB (%+.1f)",
			z, zm.TileRequests, float64(zm.TotalSize)/1024, zm.TotalTimeMs, zm.NetworkTimeMs, zm.AvgLatencyMs, zm.JSHeapUsedMB, zm.JSHeapDeltaMB)

		// Breather to let user see the zoom state before next jump
		chromedp.Run(ctx, chromedp.Sleep(3*time.Second))
	}

	log.Printf("Zoom benchmark complete. Waiting 5s for visual finish...")
	chromedp.Run(ctx, chromedp.Sleep(5*time.Second))

	// ------------------------------------------
	// Process Metrics
	// ------------------------------------------

	var pageLoad PageLoadMetrics
	pageLoad.DNSMs = getFloat(perfData, "dns_ms")
	pageLoad.TCPMs = getFloat(perfData, "tcp_ms")
	pageLoad.TTFBMs = getFloat(perfData, "ttfb_ms")
	pageLoad.DOMContentLoadedMs = getFloat(perfData, "dom_content_loaded_ms")
	pageLoad.FullLoadMs = getFloat(perfData, "full_load_ms")
	pageLoad.FirstContentfulPaintMs = getFloat(perfData, "fcp_ms")
	pageLoad.LargestContentfulPaintMs = getFloat(perfData, "lcp_ms")
	pageLoad.CumulativeLayoutShift = getFloat(perfData, "cls")
	pageLoad.MapReadyMs = getFloat(perfData, "map_ready_ms")

	// Calculate Layer Metrics
	var layerMetrics LayerMetrics
	// Calculate Layer Metrics using a mix of Go tracking (for absolute duration)
	// and JS readiness (for when it actually showed up on map)
	calcLayer := func(id, category string) SingleLayerMetric {
		ready := getFloat(perfData, "layer_"+id+"_ready_ms")

		var m SingleLayerMetric
		m.ReadyTimeMs = ready

		mu.Lock()
		var firstStart, lastEnd time.Time
		var totalDur time.Duration
		var count int
		for _, req := range requestLog {
			// CRITICAL: Only count requests that finished BEFORE the visual "ready" time.
			// This prevents background worker activity (pmtiles) from bloating the network metric.
			relEnd := float64(req.End.Sub(startTime).Milliseconds())
			if req.Category == category && relEnd <= ready {
				if firstStart.IsZero() || req.Start.Before(firstStart) {
					firstStart = req.Start
				}
				if lastEnd.IsZero() || req.End.After(lastEnd) {
					lastEnd = req.End
				}
				totalDur += req.End.Sub(req.Start)
				count++
			}
		}
		mu.Unlock()

		if !firstStart.IsZero() && !lastEnd.IsZero() {
			m.NetworkTimeMs = float64(lastEnd.Sub(firstStart).Milliseconds())
		} else {
			m.NetworkTimeMs = 0
		}
		if count > 0 {
			m.AvgLatencyMs = float64(totalDur.Milliseconds()) / float64(count)
		}

		m.RenderTimeMs = ready - m.NetworkTimeMs
		if m.RenderTimeMs < 0 {
			m.RenderTimeMs = 0
		}
		return m
	}

	layerMetrics.Protomaps = calcLayer("protomaps", "protomaps")
	layerMetrics.CountiesSimplified = calcLayer("counties-simplified", "counties-simplified")
	layerMetrics.CountiesFull = calcLayer("counties-full", "counties-full")

	// Basemap Metrics - use tracker data which is populated by the event handler
	pmTracker := trackers["protomaps"]
	var basemap BasemapMetrics
	basemap.TotalRequests = pmTracker.TotalRequests
	basemap.TotalSize = pmTracker.TotalSize
	basemap.NetworkLoadTimeMs = layerMetrics.Protomaps.NetworkTimeMs

	// Calculate avg latency from request log for protomaps
	var pmTotalDur time.Duration
	var pmCount int
	for _, req := range requestLog {
		if req.Category == "protomaps" {
			pmTotalDur += req.End.Sub(req.Start)
			pmCount++
		}
	}
	if pmCount > 0 {
		basemap.AvgDurationMs = float64(pmTotalDur.Milliseconds()) / float64(pmCount)
	}

	log.Println("Benchmark complete!")

	report := &BenchmarkReport{
		TestName:     "Performance Benchmark",
		Timestamp:    startTime,
		URL:          url,
		Success:      true,
		PageLoad:     pageLoad,
		LayerMetrics: layerMetrics,
		Basemap:      basemap,
		ZoomMetrics:  zoomMetrics,
		Duration:     float64(time.Since(startTime).Milliseconds()),
	}

	return report, nil
}

// getFloat helper...
func getFloat(m map[string]interface{}, key string) float64 {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case int64:
			return float64(v)
		}
	}
	return 0.0
}

func SetupBenchmarkLogger(timestamp time.Time) (cleanup func(), err error) {
	if err := os.MkdirAll("logs/benchmarks", 0755); err != nil {
		return nil, fmt.Errorf("failed to create benchmarks directory: %w", err)
	}

	filename := fmt.Sprintf("logs/benchmarks/benchmark_%s.log",
		timestamp.Format("2006-01-02_150405"))

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	multiWriter := io.MultiWriter(os.Stdout, file)
	log.SetOutput(multiWriter)

	cleanup = func() {
		log.SetOutput(os.Stdout)
		file.Close()
	}

	return cleanup, nil
}

func PrintBenchmarkSummary(report *BenchmarkReport) {
	w := log.Writer()
	fmt.Fprintln(w, "\n========== PERFORMANCE BENCHMARK ==========")
	fmt.Fprintf(w, "Test:      %s\n", report.TestName)
	fmt.Fprintf(w, "Timestamp: %s\n", report.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "URL:       %s\n", report.URL)

	fmt.Fprintln(w, "\n--- Web Vitals ---")
	fmt.Fprintf(w, "FCP:             %6.1f ms\n", report.PageLoad.FirstContentfulPaintMs)
	fmt.Fprintf(w, "LCP:             %6.1f ms\n", report.PageLoad.LargestContentfulPaintMs)
	fmt.Fprintf(w, "CLS:             %6.3f\n", report.PageLoad.CumulativeLayoutShift)

	fmt.Fprintln(w, "\n--- Page Load Metrics ---")
	fmt.Fprintf(w, "DNS:             %6.1f ms\n", report.PageLoad.DNSMs)
	fmt.Fprintf(w, "TCP:             %6.1f ms\n", report.PageLoad.TCPMs)
	fmt.Fprintf(w, "TTFB:            %6.1f ms\n", report.PageLoad.TTFBMs)
	fmt.Fprintf(w, "DOM Ready:       %6.1f ms\n", report.PageLoad.DOMContentLoadedMs)
	fmt.Fprintf(w, "Full Load:       %6.1f ms\n", report.PageLoad.FullLoadMs)
	fmt.Fprintf(w, "Map Complete:    %6.1f ms (Full Render)\n", report.PageLoad.MapReadyMs)

	fmt.Fprintln(w, "\n--- Basemap Stats ---")
	fmt.Fprintf(w, "Requests:        %d\n", report.Basemap.TotalRequests)
	fmt.Fprintf(w, "Total Size:      %.2f MB\n", float64(report.Basemap.TotalSize)/(1024*1024))
	fmt.Fprintf(w, "Avg Req Time:    %6.1f ms\n", report.Basemap.AvgDurationMs)

	fmt.Fprintln(w, "\n--- Layer Performance (Network vs Render) ---")
	fmt.Fprintf(w, "%-20s | %-12s | %-12s | %-12s | %-12s\n", "Layer", "Net Span", "Avg Latency", "Render/Wait", "Total Ready")
	fmt.Fprintln(w, "-------------------------------------------------------------------------------------")

	printL := func(name string, m SingleLayerMetric) {
		fmt.Fprintf(w, "%-20s | %8.1f ms | %8.1f ms | %8.1f ms | %8.1f ms\n",
			name, m.NetworkTimeMs, m.AvgLatencyMs, m.RenderTimeMs, m.ReadyTimeMs)
	}

	printL("Protomaps (Base)", report.LayerMetrics.Protomaps)
	if report.LayerMetrics.CountiesSimplified.ReadyTimeMs > 0 {
		printL("Counties (Simp)", report.LayerMetrics.CountiesSimplified)
	}
	if report.LayerMetrics.CountiesFull.ReadyTimeMs > 0 {
		printL("Counties (Full)", report.LayerMetrics.CountiesFull)
	}

	if len(report.ZoomMetrics) > 0 {
		fmt.Fprintln(w, "\n--- Zoom Level Parcel Metrics (Atlanta) ---")
		fmt.Fprintf(w, "%-6s | %-10s | %-12s | %-12s | %-12s | %-12s | %-12s | %-12s\n", "Zoom", "Tiles", "Net Span", "Avg Latency", "Render", "Total", "Heap Used", "Heap Delta")
		fmt.Fprintln(w, "---------------------------------------------------------------------------------------------------------------------")
		for _, z := range report.ZoomMetrics {
			fmt.Fprintf(w, "z%-5d | %-10d | %8.1f ms | %8.1f ms | %8.1f ms | %8.1f ms | %8.1f MB | %+8.1f MB\n",
				z.ZoomLevel, z.TileRequests, z.NetworkTimeMs, z.AvgLatencyMs, z.RenderTimeMs, z.TotalTimeMs, z.JSHeapUsedMB, z.JSHeapDeltaMB)
		}
	}

	fmt.Fprintf(w, "\nTotal Test Duration: %.2f seconds\n", report.Duration/1000)
	fmt.Fprintln(w, "===========================================\n")
}
