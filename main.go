package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/html"
)

// WgetClone represents the main application
type WgetClone struct {
	client      *http.Client
	interrupted bool
	mutex       sync.RWMutex
}

// NewWgetClone creates a new instance
func NewWgetClone() *WgetClone {
	client := &http.Client{
		// No timeout - let downloads run as long as needed
	}

	return &WgetClone{
		client: client,
	}
}

// SetupSignalHandling sets up graceful shutdown
func (w *WgetClone) SetupSignalHandling() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		w.mutex.Lock()
		w.interrupted = true
		w.mutex.Unlock()
		fmt.Println("\nDownload interrupted by user")
		os.Exit(1)
	}()
}

// IsInterrupted checks if the operation was interrupted
func (w *WgetClone) IsInterrupted() bool {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.interrupted
}

// ProgressWriter wraps an io.Writer to show download progress
type ProgressWriter struct {
	writer     io.Writer
	total      int64
	written    int64
	filename   string
	startTime  time.Time
	lastUpdate time.Time
	barWidth int
}

func NewProgressWriter(writer io.Writer, total int64, filename string) *ProgressWriter {
	return &ProgressWriter{
		writer:     writer,
		total:      total,
		filename:   filename,
		startTime:  time.Now(),
		lastUpdate: time.Now(),
		barWidth: 50,
	}
}

func (p *ProgressWriter) Write(data []byte) (int, error) {
	n, err := p.writer.Write(data)
	p.written += int64(n)

	// Update progress every 100ms
	if time.Since(p.lastUpdate) > 100*time.Millisecond {
		p.showProgress()
		p.lastUpdate = time.Now()
	}

	return n, err
}

func (p *ProgressWriter) showProgress() {
	fmt.Print("\r\033[K")
	if p.total > 0 {
		percentage := float64(p.written) / float64(p.total) * 100
		elapsed := time.Since(p.startTime)
		speed := float64(p.written) / elapsed.Seconds()

		// Visual progress bar
		barProgress := int(float64(p.barWidth) * percentage / 100)
		bar := strings.Repeat("=", barProgress)
		if barProgress < p.barWidth {
			bar += ">" + strings.Repeat(" ", p.barWidth-barProgress-1)
		}

		fmt.Printf("%s %3.0f%% [%s] %s/%s %.2fKB/s",
			p.filename,
			percentage,
			bar,
			formatBytes(p.written),
			formatBytes(p.total),
			speed/1024)
	} else {
		elapsed := time.Since(p.startTime)
		speed := float64(p.written) / elapsed.Seconds()
		fmt.Printf("%s %s %.2fKB/s",
			p.filename,
			formatBytes(p.written),
			speed/1024)
	}
}

func (p *ProgressWriter) Finish() {
	p.showProgress()
	fmt.Println()
}

// formatBytes converts bytes to human readable format
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

// parseRateLimit parses rate limit strings like "200k", "2M"
func parseRateLimit(rateLimitStr string) (int64, error) {
	if rateLimitStr == "" {
		return 0, nil
	}

	re := regexp.MustCompile(`^(\d+(?:\.\d+)?)(k|m|K|M)?$`)
	matches := re.FindStringSubmatch(rateLimitStr)
	if len(matches) < 2 {
		return 0, fmt.Errorf("invalid rate limit format: %s", rateLimitStr)
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, err
	}

	if len(matches) > 2 {
		switch strings.ToLower(matches[2]) {
		case "k":
			value *= 1024
		case "m":
			value *= 1024 * 1024
		}
	}

	return int64(value), nil
}

// RateLimitedReader wraps an io.Reader to limit read speed
type RateLimitedReader struct {
	reader    io.Reader
	rateLimit int64
	lastRead  time.Time
}

func NewRateLimitedReader(reader io.Reader, rateLimit int64) *RateLimitedReader {
	return &RateLimitedReader{
		reader:    reader,
		rateLimit: rateLimit,
		lastRead:  time.Now(),
	}
}

func (r *RateLimitedReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)

	if r.rateLimit > 0 && n > 0 {
		expectedDuration := time.Duration(int64(n)*int64(time.Second)) / time.Duration(r.rateLimit)
		elapsed := time.Since(r.lastRead)

		if elapsed < expectedDuration {
			time.Sleep(expectedDuration - elapsed)
		}
		r.lastRead = time.Now()
	}

	return n, err
}

// DownloadFile downloads a single file
func (w *WgetClone) DownloadFile(urlStr, outputPath, directory string, rateLimit int64) error {
	startTime := time.Now()
	fmt.Printf("Starting download at %s\n", startTime.Format("2006-01-02 15:04:05"))

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	req.Header.Set("User-Agent", "Go-Wget-Clone/1.0")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	fmt.Printf("Response received: %d %s\n", resp.StatusCode, resp.Status)

	contentLength := resp.ContentLength
	if contentLength > 0 {
		fmt.Printf("Content size: %s\n", formatBytes(contentLength))
	}

	// Determine output path
	if outputPath == "" {
		parsedURL, _ := url.Parse(urlStr)
		outputPath = path.Base(parsedURL.Path)
		if outputPath == "" || outputPath == "/" {
			outputPath = "index.html"
		}
	}

	if directory != "" {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
		outputPath = filepath.Join(directory, outputPath)
	}

	fmt.Printf("Saving to: %s\n", outputPath)

	// Create output file
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Set up progress tracking and rate limiting
	var reader io.Reader = resp.Body
	if rateLimit > 0 {
		reader = NewRateLimitedReader(reader, rateLimit)
	}

	progress := NewProgressWriter(file, contentLength, filepath.Base(outputPath))

	// Copy with progress
	written, err := io.Copy(progress, reader)
	progress.Finish()

	if err != nil {
		if w.IsInterrupted() {
			return fmt.Errorf("download interrupted")
		}
		return fmt.Errorf("download failed: %w", err)
	}

	endTime := time.Now()
	fmt.Printf("Downloaded successfully: %s\n", urlStr)
	fmt.Printf("Finished at %s\n", endTime.Format("2006-01-02 15:04:05"))
	fmt.Printf("Total downloaded: %s\n", formatBytes(written))

	return nil
}

// BackgroundDownload starts a download in the background
func (w *WgetClone) BackgroundDownload(urlStr, outputPath, directory string, rateLimit string) error {
	logFile := "wget-log"

	args := []string{os.Args[0], urlStr}
	if outputPath != "" {
		args = append(args, "-O", outputPath)
	}
	if directory != "" {
		args = append(args, "-P", directory)
	}
	if rateLimit != "" {
		args = append(args, "--rate-limit", rateLimit)
	}

	cmd := exec.Command(args[0], args[1:]...)

	logFileHandle, err := os.Create(logFile)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	defer logFileHandle.Close()

	cmd.Stdout = logFileHandle
	cmd.Stderr = logFileHandle

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start background process: %w", err)
	}

	fmt.Printf("Background download started (PID: %d)\n", cmd.Process.Pid)
	fmt.Printf("Output will be written to '%s'\n", logFile)

	return nil
}

// DownloadMultipleFiles downloads multiple files concurrently
func (w *WgetClone) DownloadMultipleFiles(urls []string, maxConcurrent int) error {
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex
	successful := 0

	for _, urlStr := range urls {
		if w.IsInterrupted() {
			break
		}

		wg.Add(1)
		go func(url string) {
			defer wg.Done()

			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			if err := w.DownloadFile(url, "", "", 0); err != nil {
				fmt.Printf("Error downloading %s: %v\n", url, err)
			} else {
				mu.Lock()
				successful++
				mu.Unlock()
			}
		}(urlStr)
	}

	wg.Wait()
	fmt.Printf("\nDownload summary: %d/%d files downloaded successfully\n", successful, len(urls))

	return nil
}

// extractLinks extracts links from HTML content
func extractLinks(htmlContent, baseURL string) ([]string, error) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil, err
	}

	var links []string
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.ElementNode {
			var attrName string
			switch n.Data {
			case "a", "link":
				attrName = "href"
			case "img", "script":
				attrName = "src"
			}

			if attrName != "" {
				for _, attr := range n.Attr {
					if attr.Key == attrName {
						if fullURL, err := url.Parse(attr.Val); err == nil {
							if base, err := url.Parse(baseURL); err == nil {
								resolved := base.ResolveReference(fullURL)
								links = append(links, resolved.String())
							}
						}
						break
					}
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}

	extract(doc)
	return links, nil
}

// shouldReject checks if a URL should be rejected based on filters
func shouldReject(urlStr string, reject, exclude []string) bool {
	for _, ext := range reject {
		if strings.HasSuffix(urlStr, ext) {
			return true
		}
	}
	for _, pattern := range exclude {
		if strings.Contains(urlStr, pattern) {
			return true
		}
	}

	return false
}

// MirrorWebsite mirrors a website recursively
func (w *WgetClone) MirrorWebsite(urlStr, baseURL string, visited map[string]bool, reject, exclude []string, maxDepth, currentDepth int) error {
	if currentDepth > maxDepth || visited[urlStr] || w.IsInterrupted() {
		return nil
	}

	visited[urlStr] = true

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", "Go-Wget-Clone/1.0")

	resp, err := w.client.Do(req)
	if err != nil {
		fmt.Printf("Error accessing %s: %v\n", urlStr, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		fmt.Printf("404 Not Found: %s\n", urlStr)
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("HTTP %d for %s\n", resp.StatusCode, urlStr)
		return nil
	}

	contentType := resp.Header.Get("Content-Type")

	// Handle HTML content
	if strings.Contains(contentType, "text/html") {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		content := string(body)

		// Extract and process links
		links, err := extractLinks(content, baseURL)
		if err == nil {
			baseURLParsed, _ := url.Parse(baseURL)

			for _, link := range links {
				if shouldReject(link, reject, exclude) {
					continue
				}

				linkParsed, err := url.Parse(link)
				if err != nil {
					continue
				}

				// Only follow links within the same domain
				if linkParsed.Host == baseURLParsed.Host {
					// Determine if this is a page or resource
					ext := strings.ToLower(filepath.Ext(linkParsed.Path))
					if ext == "" || ext == ".html" || ext == ".htm" {
						// Recursively mirror pages
						w.MirrorWebsite(link, baseURL, visited, reject, exclude, maxDepth, currentDepth+1)
					} else {
						// Download resources directly
						if !visited[link] {
							visited[link] = true
							w.DownloadFile(link, "", "", 0)
						}
					}
				}
			}
		}

		// Save HTML file
		parsedURL, _ := url.Parse(urlStr)
		outputPath := strings.TrimPrefix(parsedURL.Path, "/")
		if outputPath == "" {
			outputPath = "index.html"
		} else if filepath.Ext(outputPath) == "" {
			outputPath = filepath.Join(outputPath, "index.html")
		}

		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err == nil {
			if file, err := os.Create(outputPath); err == nil {
				defer file.Close()
				file.WriteString(content)
			}
		}
	} else {
		// Download binary files directly
		parsedURL, _ := url.Parse(urlStr)
		outputPath := strings.TrimPrefix(parsedURL.Path, "/")
		if outputPath == "" {
			outputPath = "index"
		}

		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err == nil {
			if file, err := os.Create(outputPath); err == nil {
				defer file.Close()
				io.Copy(file, resp.Body)
			}
		}
	}

	return nil
}

// Mirror starts website mirroring
func (w *WgetClone) Mirror(urlStr string, reject, exclude []string) error {
	visited := make(map[string]bool)
	fmt.Printf("Starting to mirror: %s\n", urlStr)

	err := w.MirrorWebsite(urlStr, urlStr, visited, reject, exclude, 3, 0)

	fmt.Printf("Mirroring completed. Visited %d URLs.\n", len(visited))
	return err
}

func main() {
	var (
		output        = flag.String("O", "", "Output filename")
		directory     = flag.String("P", "", "Directory to save files")
		rateLimit     = flag.String("rate-limit", "", "Rate limit (e.g., 200k, 2M)")
		background    = flag.Bool("B", false, "Download in background")
		inputFile     = flag.String("i", "", "File containing URLs to download")
		mirror        = flag.Bool("mirror", false, "Mirror website")
		reject        = flag.String("R", "", "Comma-separated file extensions to reject")
		exclude       = flag.String("X", "", "Comma-separated paths to exclude")
		maxConcurrent = flag.Int("max-concurrent", 5, "Maximum concurrent downloads")
	)

	flag.Parse()

	args := flag.Args()
	if len(args) == 0 && *inputFile == "" {
		fmt.Println("Usage: go-wget [options] URL")
		fmt.Println("       go-wget -i input-file")
		flag.PrintDefaults()
		os.Exit(1)
	}

	wget := NewWgetClone()
	wget.SetupSignalHandling()

	var err error

	if *mirror {
		if len(args) == 0 {
			fmt.Println("URL required for mirroring")
			os.Exit(1)
		}

		var rejectList, excludeList []string
		if *reject != "" {
			rejectList = strings.Split(*reject, ",")
		}
		if *exclude != "" {
			excludeList = strings.Split(*exclude, ",")
		}

		err = wget.Mirror(args[0], rejectList, excludeList)

	} else if *inputFile != "" {
		file, err := os.Open(*inputFile)
		if err != nil {
			fmt.Printf("Error opening input file: %v\n", err)
			os.Exit(1)
		}
		defer file.Close()

		var urls []string
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				urls = append(urls, line)
			}
		}

		if len(urls) == 0 {
			fmt.Println("No URLs found in input file")
			os.Exit(1)
		}

		err = wget.DownloadMultipleFiles(urls, *maxConcurrent)
		if err != nil {
			fmt.Printf("Error downloading files: %v\n", err)
			os.Exit(1)
		}

	} else {
		urlStr := args[0]

		if *background {
			err = wget.BackgroundDownload(urlStr, *output, *directory, *rateLimit)
		} else {
			rateLimitBytes, parseErr := parseRateLimit(*rateLimit)
			if parseErr != nil {
				fmt.Printf("Error parsing rate limit: %v\n", parseErr)
				os.Exit(1)
			}

			err = wget.DownloadFile(urlStr, *output, *directory, rateLimitBytes)
		}
	}

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
