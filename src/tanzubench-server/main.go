package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Port       int    `json:"port"`
	StaticDir  string `json:"static_dir"`
	ResultsDir string `json:"results_dir"`
	ConfigFile string `json:"config_file"`
}

type RunStatus struct {
	Running   bool      `json:"running"`
	Model     string    `json:"model,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	Log       string    `json:"log,omitempty"`
	LastRun   *RunResult `json:"last_run,omitempty"`
}

type RunResult struct {
	Model      string    `json:"model"`
	Composite  float64   `json:"composite"`
	ResultFile string    `json:"result_file"`
	FinishedAt time.Time `json:"finished_at"`
	Duration   string    `json:"duration"`
	Error      string    `json:"error,omitempty"`
}

var (
	runMu     sync.Mutex
	runStatus = RunStatus{}
	runLog    strings.Builder
)

func main() {
	configFile := flag.String("config", "", "path to config.json")
	port := flag.Int("port", 8080, "HTTP port")
	resultsDir := flag.String("results-dir", "./results", "path to results directory")
	staticDir := flag.String("static-dir", "./web/out", "path to static web files")
	flag.Parse()

	cfg := Config{
		Port:       *port,
		StaticDir:  *staticDir,
		ResultsDir: *resultsDir,
		ConfigFile: *configFile,
	}

	// API routes
	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/results", handleResults(cfg))
	http.HandleFunc("/api/run", handleRun(cfg))
	http.HandleFunc("/api/export", handleExport(cfg))
	http.HandleFunc("/api/upload", handleUpload(cfg))

	// Dashboard UI (run trigger + status)
	http.HandleFunc("/dashboard", handleDashboard)
	http.HandleFunc("/dashboard/", handleDashboard)

	// Static files (the Next.js leaderboard)
	fs := http.FileServer(http.Dir(cfg.StaticDir))
	http.Handle("/", fs)

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("TanzuBench server starting on %s", addr)
	log.Printf("  Static: %s", cfg.StaticDir)
	log.Printf("  Results: %s", cfg.ResultsDir)
	if cfg.ConfigFile != "" {
		log.Printf("  Config: %s", cfg.ConfigFile)
	}
	log.Fatal(http.ListenAndServe(addr, nil))
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	runMu.Lock()
	defer runMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runStatus)
}

func handleResults(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var results []json.RawMessage
		err := filepath.Walk(cfg.ResultsDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".json") {
				return nil
			}
			if strings.HasPrefix(info.Name(), ".") {
				return nil // skip in-progress files
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			results = append(results, json.RawMessage(data))
			return nil
		})
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

func handleRun(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}

		runMu.Lock()
		if runStatus.Running {
			runMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(409)
			json.NewEncoder(w).Encode(map[string]string{"error": "benchmark already running"})
			return
		}
		runStatus.Running = true
		runStatus.StartedAt = time.Now()
		runLog.Reset()
		runMu.Unlock()

		// Read benchmark config
		var benchConfig map[string]interface{}
		if cfg.ConfigFile != "" {
			data, err := os.ReadFile(cfg.ConfigFile)
			if err == nil {
				json.Unmarshal(data, &benchConfig)
			}
		}

		// Build command
		args := []string{
			"/var/vcap/packages/tanzubench/tools/bench_suite.py",
			"--url", getStr(benchConfig, "genai_endpoint", "http://127.0.0.1:4000"),
			"--model", getStr(benchConfig, "genai_model", "unknown"),
			"--engine", getStr(benchConfig, "genai_engine", "vllm"),
			"--foundation", getStr(benchConfig, "foundation", "local"),
			"--hardware", getStr(benchConfig, "hardware_type", "gpu"),
			"--output", cfg.ResultsDir + "/" + getStr(benchConfig, "hardware_type", "gpu") + "/",
			"--max-run-time", "7200",
			"--no-interactive",
		}

		apiKey := getStr(benchConfig, "genai_api_key", "")
		if apiKey != "" {
			args = append(args, "--api-key", apiKey)
		}

		taskTimeout := getInt(benchConfig, "task_timeout", 0)
		if taskTimeout > 0 {
			args = append(args, "--task-timeout", fmt.Sprintf("%d", taskTimeout))
		}

		if getBool(benchConfig, "suppress_thinking", true) {
			args = append(args, "--suppress-thinking")
		}

		tileVer := getStr(benchConfig, "tile_version", "")
		if tileVer != "" {
			args = append(args, "--tile-version", tileVer)
		}

		judgeEndpoint := getStr(benchConfig, "judge_endpoint", "")
		if judgeEndpoint != "" {
			args = append(args, "--judge-url", judgeEndpoint)
			args = append(args, "--judge-model", getStr(benchConfig, "judge_model", ""))
			judgeKey := getStr(benchConfig, "judge_api_key", "")
			if judgeKey != "" {
				args = append(args, "--judge-api-key", judgeKey)
			}
		}

		runMu.Lock()
		runStatus.Model = getStr(benchConfig, "genai_model", "unknown")
		runMu.Unlock()

		// Launch in background
		go func() {
			start := time.Now()
			cmd := exec.Command("python3", args...)
			cmd.Env = append(os.Environ(), "PYTHONPATH=/var/vcap/packages/tanzubench")

			stdout, _ := cmd.StdoutPipe()
			cmd.Stderr = cmd.Stdout
			cmd.Start()

			buf := make([]byte, 4096)
			for {
				n, err := stdout.Read(buf)
				if n > 0 {
					runMu.Lock()
					runLog.Write(buf[:n])
					// Keep only last 50KB of log
					if runLog.Len() > 50000 {
						s := runLog.String()
						runLog.Reset()
						runLog.WriteString(s[len(s)-40000:])
					}
					runStatus.Log = runLog.String()
					runMu.Unlock()
				}
				if err != nil {
					break
				}
			}

			cmdErr := cmd.Wait()
			duration := time.Since(start)

			runMu.Lock()
			runStatus.Running = false
			result := &RunResult{
				Model:      getStr(benchConfig, "genai_model", "unknown"),
				FinishedAt: time.Now(),
				Duration:   duration.Round(time.Second).String(),
			}
			if cmdErr != nil {
				result.Error = cmdErr.Error()
			}
			runStatus.LastRun = result
			runMu.Unlock()

			log.Printf("Benchmark run completed in %s (err: %v)", duration, cmdErr)
		}()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "started"})
	}
}

func handleExport(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Create a tar.gz of all results
		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("tanzubench-export-%d.tar.gz", time.Now().Unix()))
		cmd := exec.Command("tar", "-czf", tmpFile, "-C", cfg.ResultsDir, ".")
		if err := cmd.Run(); err != nil {
			http.Error(w, "export failed: "+err.Error(), 500)
			return
		}
		defer os.Remove(tmpFile)

		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", "attachment; filename=tanzubench-export.tar.gz")
		f, err := os.Open(tmpFile)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer f.Close()
		io.Copy(w, f)
	}
}

// Helper functions
func getStr(m map[string]interface{}, key, def string) string {
	if m == nil {
		return def
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return def
}

func getInt(m map[string]interface{}, key string, def int) int {
	if m == nil {
		return def
	}
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return def
}

func getBool(m map[string]interface{}, key string, def bool) bool {
	if m == nil {
		return def
	}
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

func handleUpload(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		// Read the JSON body
		body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024)) // 10MB max
		if err != nil {
			http.Error(w, "read error: "+err.Error(), 400)
			return
		}

		// Parse to get a filename hint
		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), 400)
			return
		}

		// Generate filename from target name + timestamp
		targetName := "unknown"
		if target, ok := result["target"].(map[string]interface{}); ok {
			if name, ok := target["name"].(string); ok {
				targetName = name
			}
		}
		hw := "cpu"
		if hardware, ok := result["hardware"].(map[string]interface{}); ok {
			if gc, ok := hardware["gpu_count"].(float64); ok && gc > 0 {
				hw = "gpu"
			}
		}

		// Create hardware subdirectory
		dir := filepath.Join(cfg.ResultsDir, hw)
		os.MkdirAll(dir, 0755)

		// Write file
		slug := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
				return r
			}
			if r >= 'A' && r <= 'Z' {
				return r + 32
			}
			return '-'
		}, targetName)
		ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
		filename := fmt.Sprintf("%s-%s.json", slug, ts)
		path := filepath.Join(dir, filename)

		if err := os.WriteFile(path, body, 0644); err != nil {
			http.Error(w, "write error: "+err.Error(), 500)
			return
		}

		log.Printf("Result uploaded: %s (%d bytes)", path, len(body))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "uploaded",
			"path":   path,
		})
	}
}
