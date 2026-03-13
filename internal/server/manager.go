// Package server manages the whisper-server child process.
//
// Rather than embedding whisper.cpp via CGo, whisker now runs the upstream
// whisper-server binary as a separate process and communicates over HTTP.
// This provides:
//   - Process isolation: a whisper crash cannot take down the bot
//   - No CGo / C toolchain dependency at Go build time
//   - Runtime configurability: model, VAD, flash-attn flags live in env vars
//   - Automatic restart on unexpected exit
package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Config holds all flags forwarded to the whisper-server binary.
// Fields map 1-to-1 to env vars; see example.env for documentation.
type Config struct {
	// BinPath is the path to the whisper-server executable.
	BinPath string // WHISPER_SERVER_BIN, default: ./bin/whisper-server

	// ListenAddr is host:port the server binds to.
	Host string // WHISPER_SERVER_HOST, default: 127.0.0.1
	Port string // WHISPER_SERVER_PORT, default: 8080

	// Inference options
	ModelPath   string // WHISPER_MODEL
	Threads     int    // WHISPER_THREADS
	BeamSize    int    // WHISPER_BEAM_SIZE
	Language    string // WHISPER_LANGUAGE, default: auto
	FlashAttn   bool   // WHISPER_FLASH_ATTN
	NoTimestamp bool   // always true — we don't need timestamps

	// VAD (Voice Activity Detection) — strips silence before inference.
	// Silero-VAD model must be present at VADModelPath.
	VAD          bool   // WHISPER_VAD
	VADModelPath string // WHISPER_VAD_MODEL
}

// ConfigFromEnv builds a Config by reading environment variables with
// sensible defaults for the i5-8250U target machine.
func ConfigFromEnv() Config {
	c := Config{
		BinPath:      envOr("WHISPER_SERVER_BIN", "./bin/whisper-server"),
		Host:         envOr("WHISPER_SERVER_HOST", "127.0.0.1"),
		Port:         envOr("WHISPER_SERVER_PORT", "8080"),
		ModelPath:    envOr("WHISPER_MODEL", "models/ggml-medium-q8_0.bin"),
		Language:     envOr("WHISPER_LANGUAGE", "auto"),
		VADModelPath: envOr("WHISPER_VAD_MODEL", "models/ggml-silero-v6.2.0.bin"),
	}

	if v := os.Getenv("WHISPER_THREADS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Threads = n
		}
	}
	if c.Threads == 0 {
		c.Threads = 4 // physical cores on i5-8250U; HT hurts on this workload
	}

	if v := os.Getenv("WHISPER_BEAM_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.BeamSize = n
		}
	}
	if c.BeamSize == 0 {
		c.BeamSize = 5 // matches Python openai-whisper default
	}

	c.FlashAttn = isTruthy(os.Getenv("WHISPER_FLASH_ATTN"))
	c.VAD = isTruthy(os.Getenv("WHISPER_VAD"))

	return c
}

// Manager owns a single whisper-server child process and restarts it on exit.
type Manager struct {
	cfg    Config
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{} // closed when the manager goroutine exits
}

// Start launches the manager goroutine. It starts whisper-server, waits for
// it to be ready, and from then on supervises it (restart on crash).
// Call Stop to shut everything down cleanly.
func Start(cfg Config) (*Manager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	// Start the server and wait for the first successful health-check before
	// returning, so the caller can immediately begin sending requests.
	if err := m.startOnce(); err != nil {
		cancel()
		return nil, err
	}

	go m.supervise()
	return m, nil
}

// Stop shuts down the manager; the child process receives SIGTERM via the
// context cancellation path.
func (m *Manager) Stop() {
	m.cancel()
	<-m.done
}

// URL returns the base URL of the managed whisper-server.
func (m *Manager) URL() string {
	return fmt.Sprintf("http://%s:%s", m.cfg.Host, m.cfg.Port)
}

// startOnce launches one whisper-server process and blocks until it responds
// on /inference (or the process exits early, or a timeout expires).
func (m *Manager) startOnce() error {
	args := m.buildArgs()
	log.Printf("[server] starting whisper-server: %s %s", m.cfg.BinPath, strings.Join(args, " "))

	cmd := exec.CommandContext(m.ctx, m.cfg.BinPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start whisper-server: %w", err)
	}

	// exited is closed as soon as the process terminates (for any reason).
	// waitReady selects on this channel so it can fail fast instead of
	// polling for the full timeout when e.g. the model file is missing.
	exited := make(chan struct{})
	go func() {
		cmd.Wait() //nolint:errcheck — we only care about the signal, not the error here
		close(exited)
	}()

	// Poll until the server accepts connections.  whisper-server loads the
	// model at startup which takes several seconds on the target hardware.
	if err := m.waitReady(60*time.Second, exited); err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("whisper-server did not become ready: %w", err)
	}
	log.Printf("[server] whisper-server ready at %s", m.URL())

	// Watch for unexpected exit and log it (context-cancelled exit is normal).
	go func() {
		<-exited
		if m.ctx.Err() == nil {
			log.Printf("[server] whisper-server exited unexpectedly")
		}
	}()

	return nil
}

// supervise restarts the server process if it exits while the manager is
// still running.  It does NOT restart on a clean shutdown (context cancelled).
func (m *Manager) supervise() {
	defer close(m.done)

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-time.After(2 * time.Second):
			// Check if the server is still alive by hitting the health path.
			if !m.isAlive() {
				if m.ctx.Err() != nil {
					return
				}
				log.Printf("[server] whisper-server unreachable — restarting...")
				if err := m.startOnce(); err != nil {
					log.Printf("[server] restart failed: %v — will retry in 10s", err)
					select {
					case <-m.ctx.Done():
						return
					case <-time.After(10 * time.Second):
					}
				}
			}
		}
	}
}

// waitReady polls GET /inference until a non-5xx response, the process exits,
// or the timeout expires — whichever comes first.
// whisper-server returns 400 for a plain GET (expects POST + file), which is
// fine — it means the server is up and listening.
func (m *Manager) waitReady(timeout time.Duration, exited <-chan struct{}) error {
	deadline := time.Now().Add(timeout)
	url := m.URL() + "/inference"
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		// Fail fast if the process already died.
		select {
		case <-exited:
			return fmt.Errorf("process exited before becoming ready (check model path and server logs above)")
		default:
		}

		resp, err := client.Get(url) //nolint:noctx
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %s waiting for %s", timeout, url)
}

// isAlive returns true if the server responds to a quick GET /inference probe.
func (m *Manager) isAlive() bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(m.URL() + "/inference")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

// buildArgs constructs the CLI flags for whisper-server from the Config.
func (m *Manager) buildArgs() []string {
	c := m.cfg
	args := []string{
		"--model", c.ModelPath,
		"--host", c.Host,
		"--port", c.Port,
		"--threads", strconv.Itoa(c.Threads),
		"--beam-size", strconv.Itoa(c.BeamSize),
		"--language", c.Language,
		"--no-timestamps",  // we don't need per-segment timestamps
		"--convert",        // let the server invoke ffmpeg for format conversion
	}

	if c.FlashAttn {
		args = append(args, "--flash-attn")
	}

	if c.VAD {
		args = append(args, "--vad", "--vad-model", c.VADModelPath)
	}

	return args
}

// envOr returns the environment variable value or fallback if unset/empty.
func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// isTruthy returns true for any non-empty value except "0" and "false".
func isTruthy(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v != "" && v != "0" && v != "false"
}
