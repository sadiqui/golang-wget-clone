package main

import (
	"bufio"
	"bytes"
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
	client        *http.Client
	interrupted   bool
	mutex         sync.RWMutex
	mirrorBaseDir string
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
	writer      io.Writer
	total       int64
	written     int64
	filename    string
	startTime   time.Time
	lastUpdate  time.Time
	barWidth    int
	isMirroring bool
}

func NewProgressWriter(writer io.Writer, total int64, filename string, isMirroring bool) *ProgressWriter {
	return &ProgressWriter{
		writer:      writer,
		total:       total,
		filename:    filename,
		startTime:   time.Now(),
		lastUpdate:  time.Now(),
		barWidth:    50,
		isMirroring: isMirroring,
	}
}

func (p *ProgressWriter) Write(data []byte) (int, error) {
	n, err := p.writer.Write(data)
	p.written += int64(n)

	if !p.isMirroring { // Only show real-time progress for single non-mirror downloads
		// Update progress every 100ms
		if time.Since(p.lastUpdate) > 100*time.Millisecond {
			p.showProgress()
			p.lastUpdate = time.Now()
		}
	}

	return n, err
}

func (p *ProgressWriter) showProgress() {
	if p.isMirroring {
		// For mirroring, we just want to print a final message, not continuous updates
		return
	}

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
	if !p.isMirroring {
		p.showProgress()
		fmt.Println()
	} else {
		// For mirroring, just print a simple line completion
		fmt.Printf("Downloaded: %s\n", p.filename)
	}
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
func (w *WgetClone) DownloadFile(urlStr, outputPath, directory string, rateLimit int64, isMirroring bool) error {
	// For mirroring, suppress initial download messages to avoid clutter
	if !isMirroring {
		startTime := time.Now()
		fmt.Printf("Starting download at %s\n", startTime.Format("2006-01-02 15:04:05"))
	}

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

	initialContentLength := resp.ContentLength

	// For mirroring, suppress content details
	if !isMirroring {
		fmt.Printf("Response received: %d %s\n", resp.StatusCode, resp.Status)
		if initialContentLength > 0 {
			fmt.Printf("Content size: %s\n", formatBytes(initialContentLength))
		}
	}

	// Determine output path based on mirroring logic
	finalOutputPath := outputPath
	if isMirroring {
		parsedURL, _ := url.Parse(urlStr)
		relativeURLPath := strings.TrimPrefix(parsedURL.Path, "/")
		if strings.HasSuffix(relativeURLPath, "/") || filepath.Ext(relativeURLPath) == "" {
			relativeURLPath = filepath.Join(relativeURLPath, "index.html")
		}
		finalOutputPath = filepath.Join(w.mirrorBaseDir, parsedURL.Hostname(), relativeURLPath)
	} else if outputPath == "" {
		parsedURL, _ := url.Parse(urlStr)
		finalOutputPath = path.Base(parsedURL.Path)
		if finalOutputPath == "" || finalOutputPath == "/" {
			finalOutputPath = "index.html"
		}
	}

	if directory != "" && !isMirroring {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
		finalOutputPath = filepath.Join(directory, finalOutputPath)
	}

	// Ensure the directory for the output path exists
	dir := filepath.Dir(finalOutputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory '%s': %w", dir, err)
	}

	// Create output file (before reading body to avoid re-reading for HTML rewrite)
	file, err := os.Create(finalOutputPath)
	if err != nil {
		return fmt.Errorf("failed to create file '%s': %w", finalOutputPath, err)
	}
	defer file.Close()

	// Set up progress tracking and rate limiting
	var reader io.Reader = resp.Body
	if rateLimit > 0 {
		reader = NewRateLimitedReader(reader, rateLimit)
	}

	// Initialize progress *before* io.Copy, using the captured initialContentLength
	progress := NewProgressWriter(file, initialContentLength, filepath.Base(finalOutputPath), isMirroring)

	// Copy with progress
	written, err := io.Copy(progress, reader) // This will read the body and write to the file
	progress.Finish()                         // This will print a simple "Downloaded: X" if mirroring

	if err != nil {
		if w.IsInterrupted() {
			return fmt.Errorf("download interrupted")
		}
		return fmt.Errorf("download failed: %w", err)
	}

	if !isMirroring {
		endTime := time.Now()
		fmt.Printf("Downloaded successfully: %s\n", urlStr)
		fmt.Printf("Finished at %s\n", endTime.Format("2006-01-02 15:04:05"))
		fmt.Printf("Total downloaded: %s\n", formatBytes(written))
	}

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
func (w *WgetClone) DownloadMultipleFiles(urls []string, maxConcurrent int, directory string, rateLimit int64) error {
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex
	successful := 0

	fmt.Printf("Starting concurrent download of %d files with %d max concurrency...\n", len(urls), maxConcurrent)

	for _, urlStr := range urls {
		if w.IsInterrupted() {
			fmt.Println("Concurrent download interrupted.")
			break
		}

		wg.Add(1)
		go func(url string) {
			defer wg.Done()

			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			// For concurrent downloads, we don't pass `isMirroring=true` to DownloadFile
			// because they are individual files, not part of a recursive mirror.
			if err := w.DownloadFile(url, "", directory, rateLimit, false); err != nil {
				fmt.Printf("Error downloading %s: %v\n", url, err)
			} else {
				mu.Lock()
				successful++
				mu.Unlock()
				fmt.Printf("Finished: %s\n", url)
			}
		}(urlStr)
	}

	wg.Wait()
	fmt.Printf("\nDownload summary: %d/%d files downloaded successfully\n", successful, len(urls))

	return nil
}

// HTML rewriting utility
// rewriteHTML adjusts relative/absolute paths in HTML to be local
func rewriteHTML(content string, currentURL, baseURL string) (string, error) {
	doc, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	currentParsedURL, _ := url.Parse(currentURL)
	baseParsedURL, _ := url.Parse(baseURL)

	var rewrite func(*html.Node)
	rewrite = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for i, a := range n.Attr {
				var attrToRewrite bool
				switch n.Data {
				case "a", "link":
					attrToRewrite = (a.Key == "href")
				case "img", "script":
					attrToRewrite = (a.Key == "src")
				}

				if attrToRewrite {
					originalVal := a.Val
					parsedLink, err := url.Parse(originalVal)
					if err != nil {
						continue
					}
					resolvedURL := currentParsedURL.ResolveReference(parsedLink)
					if resolvedURL.Hostname() == baseParsedURL.Hostname() {
						relativePath := strings.TrimPrefix(resolvedURL.Path, "/")
						if strings.HasSuffix(relativePath, "/") || filepath.Ext(relativePath) == "" {
							relativePath = filepath.Join(relativePath, "index.html")
						}
						localPath := filepath.Join(resolvedURL.Hostname(), relativePath)
						currentFileLocalPath := filepath.Join(currentParsedURL.Hostname(), strings.TrimPrefix(currentParsedURL.Path, "/"))
						if strings.HasSuffix(currentFileLocalPath, "/") || filepath.Ext(currentFileLocalPath) == "" {
							currentFileLocalPath = filepath.Join(currentFileLocalPath, "index.html")
						}
						relPath, err := filepath.Rel(filepath.Dir(currentFileLocalPath), localPath)
						if err == nil {
							a.Val = relPath
							n.Attr[i] = a
						} else {
							a.Val = "/" + localPath
							n.Attr[i] = a
						}

					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			rewrite(c)
		}
	}

	rewrite(doc)

	var buf bytes.Buffer
	err = html.Render(&buf, doc)
	if err != nil {
		return "", fmt.Errorf("failed to render modified HTML: %w", err)
	}
	return buf.String(), nil
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
			case "form":
				attrName = "action"
			}

			if attrName != "" {
				for _, attr := range n.Attr {
					if attr.Key == attrName {
						if fullURL, err := url.Parse(attr.Val); err == nil {
							if base, err := url.Parse(baseURL); err == nil {
								resolved := base.ResolveReference(fullURL)
								// Only add if it's http/https and not a data URI etc.
								if resolved.Scheme == "http" || resolved.Scheme == "https" {
									links = append(links, resolved.String())
								}
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
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return true
	}
	ext := strings.ToLower(filepath.Ext(parsedURL.Path))
	for _, rExt := range reject {
		if ext == "."+strings.ToLower(rExt) {
			return true
		}
	}
	for _, pattern := range exclude {
		if strings.Contains(parsedURL.Path, pattern) {
			return true
		}
	}

	return false
}

// MirrorWebsite mirrors a website recursively
func (w *WgetClone) MirrorWebsite(urlStr, baseURL string, visited map[string]bool, reject, exclude []string, maxDepth, currentDepth int, wg *sync.WaitGroup, sem chan struct{}) {
	defer wg.Done() // Decrement counter when goroutine finishes

	if w.IsInterrupted() {
		return
	}
	if currentDepth > maxDepth {
		fmt.Printf("Skipping %s: Max depth (%d) reached.\n", urlStr, maxDepth)
		return
	}

	// Lock for `visited` map access
	w.mutex.Lock()
	if visited[urlStr] {
		w.mutex.Unlock()
		return
	}
	visited[urlStr] = true
	w.mutex.Unlock()

	fmt.Printf("Mirroring: %s (Depth: %d)\n", urlStr, currentDepth)

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		fmt.Printf("Error forming request for %s: %v\n", urlStr, err)
		return
	}

	req.Header.Set("User-Agent", "Go-Wget-Clone/1.0")

	resp, err := w.client.Do(req)
	if err != nil {
		fmt.Printf("Error accessing %s: %v\n", urlStr, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		fmt.Printf("404 Not Found: %s\n", urlStr)
		return
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("HTTP %d for %s\n", resp.StatusCode, urlStr)
		return
	}

	contentType := resp.Header.Get("Content-Type")

	// Read content fully into memory for processing (especially for HTML rewriting)
	contentBytes, err := io.ReadAll(resp.Body) // Read the entire body here
	if err != nil {
		fmt.Printf("Error reading content from %s: %v\n", urlStr, err)
		return
	}

	// Determine output path based on mirroring logic
	parsedURL, _ := url.Parse(urlStr)
	relativeURLPath := strings.TrimPrefix(parsedURL.Path, "/")
	if strings.HasSuffix(relativeURLPath, "/") || filepath.Ext(relativeURLPath) == "" {
		relativeURLPath = filepath.Join(relativeURLPath, "index.html")
	}
	// Combine with the base mirroring directory and hostname
	localFilePath := filepath.Join(w.mirrorBaseDir, parsedURL.Hostname(), relativeURLPath)

	// Ensure directory exists
	dir := filepath.Dir(localFilePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Printf("Failed to create directory '%s': %v\n", dir, err)
		return
	}

	// Handle HTML content
	if strings.Contains(contentType, "text/html") {
		contentString := string(contentBytes)

		// Extract and process links (before rewriting content for saving)
		links, err := extractLinks(contentString, baseURL)
		if err == nil {
			baseURLParsed, _ := url.Parse(baseURL)

			for _, link := range links {
				if w.IsInterrupted() {
					return
				}
				if shouldReject(link, reject, exclude) {
					continue
				}

				linkParsed, err := url.Parse(link)
				if err != nil {
					fmt.Printf("Warning: Malformed link found: %s, %v\n", link, err)
					continue
				}

				// Only follow links within the base domain being mirrored
				if linkParsed.Hostname() == baseURLParsed.Hostname() {
					// Add to waitgroup and acquire semaphore before launching goroutine
					wg.Add(1)
					sem <- struct{}{}
					go func(l string) {
						defer func() { <-sem }() // Release semaphore
						w.MirrorWebsite(l, baseURL, visited, reject, exclude, maxDepth, currentDepth+1, wg, sem)
					}(link)
				}
			}
		} else {
			fmt.Printf("Error extracting links from %s: %v\n", urlStr, err)
		}

		// Rewrite HTML content after links have been processed
		rewrittenContent, rewriteErr := rewriteHTML(contentString, urlStr, baseURL)
		if rewriteErr != nil {
			fmt.Printf("Error rewriting HTML for %s: %v\n", urlStr, rewriteErr)
			// Continue saving original if rewrite fails
		} else {
			contentBytes = []byte(rewrittenContent) // Update contentBytes with rewritten content
		}

		// Save HTML file
		file, err := os.Create(localFilePath)
		if err != nil {
			fmt.Printf("Failed to create HTML file '%s': %v\n", localFilePath, err)
			return
		}
		defer file.Close()

		// Use ProgressWriter for saving HTML, passing len(contentBytes) as total
		htmlProgressWriter := NewProgressWriter(file, int64(len(contentBytes)), filepath.Base(localFilePath), true)
		_, err = htmlProgressWriter.Write(contentBytes) // Directly write the bytes
		htmlProgressWriter.Finish()                     // Trigger final output for this file

		if err != nil {
			fmt.Printf("Failed to write to HTML file '%s': %v\n", localFilePath, err)
		}
	} else {
		// Save non-HTML files directly
		file, err := os.Create(localFilePath)
		if err != nil {
			fmt.Printf("Failed to create file '%s': %v\n", localFilePath, err)
			return
		}
		defer file.Close()

		// Use ProgressWriter for saving binary, passing len(contentBytes) as total
		binaryProgressWriter := NewProgressWriter(file, int64(len(contentBytes)), filepath.Base(localFilePath), true)
		_, err = binaryProgressWriter.Write(contentBytes) // Directly write the bytes
		binaryProgressWriter.Finish()                     // Trigger final output for this file

		if err != nil {
			fmt.Printf("Failed to write to file '%s': %v\n", localFilePath, err)
		}
	}
}

// Mirror starts website mirroring
func (w *WgetClone) Mirror(urlStr string, reject, exclude []string, maxDepth, maxConcurrent int) error {
	visited := make(map[string]bool)
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrent) // Semaphore for concurrency control

	// Set the base directory for mirrored files
	parsedBaseURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid base URL for mirroring: %w", err)
	}
	// Default mirror directory is current_dir/domain_name
	w.mirrorBaseDir = parsedBaseURL.Hostname()
	if w.mirrorBaseDir == "" {
		w.mirrorBaseDir = "mirrored_site" // Fallback if hostname is empty (e.g., file:// URLs)
	}
	fmt.Printf("Starting to mirror '%s' into directory '%s'\n", urlStr, w.mirrorBaseDir)

	wg.Add(1)
	sem <- struct{}{} // Acquire initial semaphore
	go func() {
		defer func() { <-sem }() // Release initial semaphore
		w.MirrorWebsite(urlStr, urlStr, visited, reject, exclude, maxDepth, 0, &wg, sem)
	}()

	wg.Wait() // Wait for all mirroring goroutines to complete

	fmt.Printf("\nMirroring completed. Visited %d URLs.\n", len(visited))
	return nil
}

func main() {
	var (
		output        = flag.String("O", "", "Output filename")
		directory     = flag.String("P", "", "Directory to save files")
		rateLimit     = flag.String("rate-limit", "", "Rate limit (e.g., 200k, 2M)")
		background    = flag.Bool("B", false, "Download in background")
		inputFile     = flag.String("i", "", "File containing URLs to download")
		mirror        = flag.Bool("mirror", false, "Mirror website")
		reject        = flag.String("R", "", "Comma-separated file extensions to reject") // mirror option
		exclude       = flag.String("X", "", "Comma-separated paths to exclude")          // mirror option
		maxDepth      = flag.Int("l", 3, "Max recursion depth for mirroring")             // mirror option
		maxConcurrent = flag.Int("max-concurrent", 5, "Maximum concurrent downloads for -i and --mirror")
		// Possible combinations: (`-i` with `-P`, and `--rate-limit` with `-O`)
	)

	flag.Parse()

	args := flag.Args()
	if len(args) == 0 && *inputFile == "" && !*mirror {

		fmt.Println(`
go-wget - A simple wget clone in Go for downloading files and mirroring websites.

Usage:
  ./wget [options] URL                Download a single URL.
  ./wget -i input-file [options]      Download multiple URLs listed in a file.
  ./wget --mirror URL [options]       Mirror an entire website recursively.

Options:`)
		flag.PrintDefaults()

		fmt.Print(`
Examples:
  ./wget https://example.com/index.html
  ./wget -i urls.txt -P downloads --rate-limit 5k
  ./wget --mirror -X "/anything,/static" -R "png,jpg,ico" https://httpbin.org
`)

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
			// Split by comma and trim spaces for extensions
			rejectList = strings.Split(*reject, ",")
			for i := range rejectList {
				rejectList[i] = strings.TrimSpace(rejectList[i])
			}
		}
		if *exclude != "" {
			// Split by comma and trim spaces for paths
			excludeList = strings.Split(*exclude, ",")
			for i := range excludeList {
				excludeList[i] = strings.TrimSpace(excludeList[i])
			}
		}

		err = wget.Mirror(args[0], rejectList, excludeList, *maxDepth, *maxConcurrent)

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

		// Parse rate limit here
		rateLimitBytes, parseErr := parseRateLimit(*rateLimit)
		if parseErr != nil {
			fmt.Printf("Error parsing rate limit: %v\n", parseErr)
			os.Exit(1)
		}

		err = wget.DownloadMultipleFiles(urls, *maxConcurrent, *directory, rateLimitBytes)
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

			err = wget.DownloadFile(urlStr, *output, *directory, rateLimitBytes, false)
		}
	}

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
