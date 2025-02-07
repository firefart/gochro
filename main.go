package main

// shell switches:
// https://source.chromium.org/chromium/chromium/src/+/main:headless/app/headless_shell_switches.cc

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
)

const (
	chromiumPath           = "/usr/bin/chromium-browser"
	defaultGracefulTimeout = 5 * time.Second
)

var (
	debugOutput      = false
	ignoreCertErrors = true
	proxyServer      = ""
	disableSandbox   = false
)

type application struct{}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func main() {
	var host string
	var wait time.Duration
	flag.StringVar(&host, "host", "127.0.0.1:8080", "IP and Port to bind to")
	flag.BoolVar(&ignoreCertErrors, "ignore-cert-errors", true, "Ignore Certificate Errors when taking screenshots of fetching ressources")
	flag.BoolVar(&debugOutput, "debug", false, "Enable DEBUG mode")
	flag.BoolVar(&disableSandbox, "disable-sandbox", false, "Disable chromium sandbox")
	flag.StringVar(&proxyServer, "proxy", "", "Proxy Server to use for chromium. Please use format IP:PORT without a protocol.")
	flag.DurationVar(&wait, "graceful-timeout", defaultGracefulTimeout, "the duration for which the server gracefully wait for existing connections to finish - e.g. 15s or 1m")
	flag.Parse()

	log.SetOutput(os.Stdout)
	log.SetLevel(log.InfoLevel)
	if debugOutput {
		log.SetLevel(log.DebugLevel)
	}

	app := &application{}

	srv := &http.Server{
		Addr:    host,
		Handler: app.routes(),
	}
	log.Infof("Starting server on %s", host)
	if debugOutput {
		log.Debug("DEBUG mode enabled")
	}

	// continuously print number of goroutines in debug mode
	if debugOutput {
		go func() {
			goRoutineTicker := time.NewTicker(3 * time.Second)
			defer goRoutineTicker.Stop()
			for range goRoutineTicker.C {
				log.Debugf("number of goroutines: %d", runtime.NumGoroutine())
			}
		}()
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Error(err)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
	<-c
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error(err)
	}
	log.Info("shutting down")
	os.Exit(0)
}

func (app *application) routes() http.Handler {
	r := mux.NewRouter()
	r.Use(app.loggingMiddleware)
	r.Use(app.recoverPanic)
	r.HandleFunc("/screenshot", app.errorHandler(app.screenshot))
	r.HandleFunc("/html2pdf", app.errorHandler(app.html2pdf))
	r.HandleFunc("/url2pdf", app.errorHandler(app.url2pdf))
	r.HandleFunc("/html", app.errorHandler(app.html))
	r.PathPrefix("/").HandlerFunc(app.catchAllHandler)
	return r
}

func (app *application) catchAllHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Connection", "close")
	w.WriteHeader(http.StatusNotFound)
	if _, err := w.Write([]byte("Not found")); err != nil {
		log.Error(err)
	}
}

func (app *application) loggingMiddleware(next http.Handler) http.Handler {
	return handlers.CombinedLoggingHandler(os.Stdout, next)
}

func (app *application) toImage(ctx context.Context, url string, w, h *int, userAgent *string) ([]byte, error) {
	return app.execChrome(ctx, "screenshot", url, w, h, userAgent)
}

func (app *application) toPDF(ctx context.Context, url string, w, h *int, userAgent *string) ([]byte, error) {
	return app.execChrome(ctx, "pdf", url, w, h, userAgent)
}

func (app *application) toHTML(ctx context.Context, url string, w, h *int, userAgent *string) ([]byte, error) {
	return app.execChrome(ctx, "html", url, w, h, userAgent)
}

func (app *application) execChrome(ctxMain context.Context, action, url string, w, h *int, userAgent *string) ([]byte, error) {
	args := []string{
		"--headless=new", // https://developer.chrome.com/articles/new-headless/
		"--disable-gpu",
		"--disable-software-rasterizer",
		"--virtual-time-budget=55000", // 55 secs, context timeout is 1 minute
		"--disable-dev-shm-usage",
		"--hide-scrollbars",
		"--disable-crash-reporter",
		"--block-new-web-contents",
	}

	if w != nil && *w > 0 && h != nil && *h > 0 {
		args = append(args, fmt.Sprintf("--window-size=%d,%d", *w, *h))
	}

	if debugOutput {
		args = append(args, "--enable-logging")
		args = append(args, "--v=1")
	}

	if ignoreCertErrors {
		args = append(args, "--ignore-certificate-errors")
	}

	if disableSandbox {
		args = append(args, "--no-sandbox")
	}

	if proxyServer != "" {
		args = append(args, fmt.Sprintf("--proxy-server=%s", proxyServer))
	}

	if userAgent != nil && len(*userAgent) > 0 {
		args = append(args, fmt.Sprintf("--user-agent=%s", *userAgent))
	}

	switch action {
	case "screenshot":
		args = append(args, "--screenshot")
	case "pdf":
		args = append(args, "--print-to-pdf", "--no-pdf-header-footer")
	case "html":
		args = append(args, "--dump-dom")
	default:
		return nil, fmt.Errorf("unknown action %q", action)
	}

	// last parameter is the url
	args = append(args, url)

	tmpdir := path.Join(os.TempDir(), fmt.Sprintf("chrome_%s", randStringRunes(10))) // nolint:gomnd
	err := os.Mkdir(tmpdir, os.ModePerm)
	if err != nil {
		return nil, fmt.Errorf("could not create dir %q: %w", tmpdir, err)
	}
	defer os.RemoveAll(tmpdir)

	ctx, cancel := context.WithTimeout(ctxMain, 1*time.Minute)
	defer cancel()

	log.Debugf("going to call chromium with the following args: %v", args)

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, chromiumPath, args...)
	cmd.Dir = tmpdir
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		killChromeProcessIfRunning(cmd)
		return nil, fmt.Errorf("could not execute command %w: %s", err, stderr.String())
	}

	log.Debugf("STDOUT: %s", out.String())
	log.Debugf("STDERR: %s", stderr.String())

	var content []byte

	switch action {
	case "screenshot":
		outfile := path.Join(tmpdir, "screenshot.png")
		content, err = os.ReadFile(outfile)
		if err != nil {
			return nil, fmt.Errorf("could not read temp file: %w", err)
		}
	case "pdf":
		outfile := path.Join(tmpdir, "output.pdf")
		content, err = os.ReadFile(outfile)
		if err != nil {
			return nil, fmt.Errorf("could not read temp file: %w", err)
		}
	case "html":
		content = out.Bytes()
	default:
		return nil, fmt.Errorf("unknown action %q", action)
	}

	killChromeProcessIfRunning(cmd)

	return content, nil
}

func killChromeProcessIfRunning(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := cmd.Process.Release(); err != nil {
		log.Error(err)
		return
	}
	if err := cmd.Process.Kill(); err != nil {
		log.Error(err)
		return
	}
}

func (app *application) logError(w http.ResponseWriter, err error, withTrace bool) {
	w.Header().Set("Connection", "close")
	errorText := fmt.Sprintf("%v", err)
	log.Error(errorText)
	if withTrace {
		log.Errorf("%s", debug.Stack())
	}
	http.Error(w, "There was an error processing your request", http.StatusInternalServerError)
}

func (app *application) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				app.logError(w, fmt.Errorf("%s", err), true)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (app *application) errorHandler(h func(*http.Request) (string, []byte, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		content, b, err := h(r)
		if err != nil {
			app.logError(w, err, false)
			return
		}
		w.Header().Set("Content-Type", content)
		_, err = w.Write(b)
		if err != nil {
			app.logError(w, err, false)
			return
		}
	}
}

func getStringParameter(r *http.Request, paramname string) *string {
	p, ok := r.URL.Query()[paramname]
	if !ok || len(p[0]) < 1 {
		return nil
	}
	ret := p[0]
	return &ret
}

func getIntParameter(r *http.Request, paramname string) (*int, error) {
	p, ok := r.URL.Query()[paramname]
	if !ok || len(p[0]) < 1 {
		return nil, nil
	}

	i, err := strconv.Atoi(p[0])
	if err != nil {
		return nil, fmt.Errorf("invalid parameter %s=%q - %w", paramname, p[0], err)
	} else if i < 1 {
		return nil, fmt.Errorf("invalid parameter %s: %q", paramname, p[0])
	}

	return &i, nil
}

// http://localhost:8080/screenshot?url=https://firefart.at&w=1024&h=768
func (app *application) screenshot(r *http.Request) (string, []byte, error) {
	url := getStringParameter(r, "url")
	if url == nil {
		return "", nil, fmt.Errorf("missing required parameter url")
	}

	// optional parameters start here
	w, err := getIntParameter(r, "w")
	if err != nil {
		return "", nil, err
	}

	h, err := getIntParameter(r, "h")
	if err != nil {
		return "", nil, err
	}

	userAgentParam := getStringParameter(r, "useragent")

	content, err := app.toImage(r.Context(), *url, w, h, userAgentParam)
	if err != nil {
		return "", nil, err
	}

	return "image/png", content, nil
}

// http://localhost:8080/html2pdf?w=1024&h=768
func (app *application) html2pdf(r *http.Request) (string, []byte, error) {
	// optional parameters start here
	w, err := getIntParameter(r, "w")
	if err != nil {
		return "", nil, err
	}

	h, err := getIntParameter(r, "h")
	if err != nil {
		return "", nil, err
	}

	userAgentParam := getStringParameter(r, "useragent")

	tmpf, err := os.CreateTemp("", "pdf.*.html")
	if err != nil {
		return "", nil, fmt.Errorf("could not create tmp file: %w", err)
	}
	defer os.Remove(tmpf.Name())

	bytes, err := io.Copy(tmpf, r.Body)
	if err != nil {
		return "", nil, fmt.Errorf("could not copy request: %w", err)
	}
	if bytes <= 0 {
		return "", nil, fmt.Errorf("please provide a valid post body")
	}

	err = tmpf.Close()
	if err != nil {
		return "", nil, fmt.Errorf("could not close tmp file: %w", err)
	}

	path, err := filepath.Abs(tmpf.Name())
	if err != nil {
		return "", nil, fmt.Errorf("could not get temp file path: %w", err)
	}

	content, err := app.toPDF(r.Context(), path, w, h, userAgentParam)
	if err != nil {
		return "", nil, err
	}

	return "application/pdf", content, nil
}

// http://localhost:8080/url2pdf?w=1024&h=768&url=https://firefart.at
func (app *application) url2pdf(r *http.Request) (string, []byte, error) {
	url := getStringParameter(r, "url")
	if url == nil {
		return "", nil, fmt.Errorf("missing required parameter url")
	}

	// optional parameters start here
	w, err := getIntParameter(r, "w")
	if err != nil {
		return "", nil, err
	}

	h, err := getIntParameter(r, "h")
	if err != nil {
		return "", nil, err
	}

	userAgentParam := getStringParameter(r, "useragent")

	content, err := app.toPDF(r.Context(), *url, w, h, userAgentParam)
	if err != nil {
		return "", nil, err
	}

	return "application/pdf", content, nil
}

// http://localhost:8080/html?url=https://firefart.at&w=1024&h=768
func (app *application) html(r *http.Request) (string, []byte, error) {
	url := getStringParameter(r, "url")
	if url == nil {
		return "", nil, fmt.Errorf("missing required parameter url")
	}

	// optional parameters start here
	w, err := getIntParameter(r, "w")
	if err != nil {
		return "", nil, err
	}

	h, err := getIntParameter(r, "h")
	if err != nil {
		return "", nil, err
	}

	userAgentParam := getStringParameter(r, "useragent")

	content, err := app.toHTML(r.Context(), *url, w, h, userAgentParam)
	if err != nil {
		return "", nil, err
	}

	return "text/plain", content, nil
}
