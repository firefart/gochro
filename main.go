package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"time"
)

const (
	chromiumPath = "/usr/bin/chromium-browser"
)

type application struct {
	infoLog  *log.Logger
	errorLog *log.Logger
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func main() {
	host := flag.String("host", "127.0.0.1:8080", "IP and Port to bind to")
	flag.Parse()

	infoLog := log.New(os.Stdout, "[INFO]\t", log.Ldate|log.Ltime)
	errorLog := log.New(os.Stderr, "[ERROR]\t", log.Ldate|log.Ltime|log.Lshortfile)

	app := &application{
		errorLog: errorLog,
		infoLog:  infoLog,
	}

	srv := &http.Server{
		Addr:     *host,
		ErrorLog: errorLog,
		Handler:  app.routes(),
	}
	app.infoLog.Printf("Starting server on %s", *host)
	app.errorLog.Fatal(srv.ListenAndServe())
}

func (app *application) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/screenshot", app.errorHandler(app.screenshot))
	mux.HandleFunc("/html2pdf", app.errorHandler(app.html2pdf))
	return app.recoverPanic(app.logRequest(mux))
}

func toImage(ctx context.Context, url string, w, h int) ([]byte, error) {
	return execChrome(ctx, "screenshot", url, w, h)
}

func toPDF(ctx context.Context, url string, w, h int) ([]byte, error) {
	return execChrome(ctx, "pdf", url, w, h)
}

func execChrome(ctxMain context.Context, action, url string, w, h int) ([]byte, error) {
	args := []string{
		"--headless",
		"--disable-gpu",
		"--disable-software-rasterizer",
		"--timeout=55000", // 55 secs, context timeout is 1 minute
		"--disable-dev-shm-usage",
		"--hide-scrollbars",
		fmt.Sprintf("--window-size=%d,%d", w, h),
	}

	switch action {
	case "screenshot":
		args = append(args, "--screenshot")
	case "pdf":
		args = append(args, "--print-to-pdf")
	default:
		return nil, fmt.Errorf("unknown action %q", action)
	}

	// last parameter is the url
	args = append(args, url)

	tmpdir := path.Join(os.TempDir(), fmt.Sprintf("chrome_%s", randStringRunes(10)))
	err := os.Mkdir(tmpdir, os.ModePerm)
	if err != nil {
		return nil, fmt.Errorf("could not create dir %q %v", tmpdir, err)
	}
	defer os.RemoveAll(tmpdir)

	ctx, cancel := context.WithTimeout(ctxMain, 1*time.Minute)
	defer cancel()

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, chromiumPath, args...)
	cmd.Dir = tmpdir
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		killChromeProcessIfRunning(cmd)
		return nil, fmt.Errorf("could not execute command %v: %s", err, stderr.String())
	}

	var outfile string

	switch action {
	case "screenshot":
		outfile = path.Join(tmpdir, "screenshot.png")
	case "pdf":
		outfile = path.Join(tmpdir, "output.pdf")
	default:
		return nil, fmt.Errorf("unknown action %q", action)
	}

	content, err := ioutil.ReadFile(outfile)
	if err != nil {
		return nil, fmt.Errorf("could not read temp file %v", err)
	}

	killChromeProcessIfRunning(cmd)

	return content, nil
}

func killChromeProcessIfRunning(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	cmd.Process.Release()
	cmd.Process.Kill()
}

func (app *application) logError(w http.ResponseWriter, err error, withTrace bool) {
	w.Header().Set("Connection", "close")
	errorText := fmt.Sprintf("%v", err)
	app.errorLog.Println(errorText)
	if withTrace {
		app.errorLog.Printf("%s", debug.Stack())
	}
	http.Error(w, "There was an error processing your request", http.StatusInternalServerError)
}

func (app *application) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app.infoLog.Printf("%s - %s %s %s", r.RemoteAddr, r.Proto, r.Method, r.URL.RequestURI())
		next.ServeHTTP(w, r)
	})
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

func getStringParameter(r *http.Request, paramname string) (string, error) {
	p, ok := r.URL.Query()[paramname]
	if !ok || len(p[0]) < 1 {
		return "", fmt.Errorf("missing parameter %s", paramname)
	}
	return p[0], nil
}

func getIntParameter(r *http.Request, paramname string) (int, error) {
	p, ok := r.URL.Query()[paramname]
	if !ok || len(p[0]) < 1 {
		return 0, fmt.Errorf("missing parameter %s", paramname)
	}

	i, err := strconv.Atoi(p[0])
	if err != nil {
		return 0, fmt.Errorf("invalid parameter %s=%q - %v", paramname, p, err)
	} else if i < 1 {
		return 0, fmt.Errorf("invalid parameter %s: %q", paramname, p)
	}

	return i, nil
}

// http://localhost:8080/screenshot?url=https://firefart.at&w=1024&h=768
func (app *application) screenshot(r *http.Request) (string, []byte, error) {
	url, err := getStringParameter(r, "url")
	if err != nil {
		return "", nil, err
	}

	w, err := getIntParameter(r, "w")
	if err != nil {
		return "", nil, err
	}

	h, err := getIntParameter(r, "h")
	if err != nil {
		return "", nil, err
	}

	content, err := toImage(r.Context(), url, w, h)
	if err != nil {
		return "", nil, err
	}

	return "image/png", content, nil
}

// http://localhost:8080/html2pdf?w=1024&h=768
func (app *application) html2pdf(r *http.Request) (string, []byte, error) {
	w, err := getIntParameter(r, "w")
	if err != nil {
		return "", nil, err
	}

	h, err := getIntParameter(r, "h")
	if err != nil {
		return "", nil, err
	}

	tmpf, err := ioutil.TempFile("", "pdf.*.html")
	if err != nil {
		return "", nil, fmt.Errorf("could not create tmp file: %v", err)
	}
	defer os.Remove(tmpf.Name())

	bytes, err := io.Copy(tmpf, r.Body)
	if err != nil {
		return "", nil, fmt.Errorf("could not copy request: %v", err)
	}
	if bytes <= 0 {
		return "", nil, fmt.Errorf("please provide a valid post body")
	}

	err = tmpf.Close()
	if err != nil {
		return "", nil, fmt.Errorf("could not close tmp file: %v", err)
	}

	path, err := filepath.Abs(tmpf.Name())
	if err != nil {
		return "", nil, fmt.Errorf("could not get temp file path: %v", err)
	}

	content, err := toPDF(r.Context(), path, w, h)
	if err != nil {
		return "", nil, err
	}

	return "application/pdf", content, nil
}
