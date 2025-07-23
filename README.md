## Available Flags

- **-B** : Download in background  
- **-O** `[string]` : Output filename  
- **-P** `[string]` : Directory to save files  
- **-i** `[string]` : File containing URLs to download  
- **-max-concurrent** `[int]` : Maximum concurrent downloads (default 5)  
- **-rate-limit** `[string]` : Rate limit (e.g., 200k, 2M)  
- **-mirror** : Mirror website  
  - **-R** `[string]` : Comma-separated file extensions to reject  
  - **-X** `[string]` : Comma-separated paths to exclude  
  - **-convert-links** `[string]` : Make files point to downloaded resources  

## Usage Examples

- **Basic examples:**

```sh
# Build Go binary
go build -o wget main.go

# Simple file download
./wget https://example.com/index.html

# File download with custom filename
./wget -O homepage.html https://example.com/index.html

# JSON download with custom name
./wget -O data.json https://httpbin.org/json

# Download to specific directory
mkdir downloads
./wget -P downloads https://httpbin.org/xml

# Multiple files download
echo -e "https://httpbin.org/robots.txt\nhttps://httpbin.org/json" > urls.txt
./wget -i urls.txt

# Test 404 page
./wget https://example.com/notfound.html

# Test invalid URL
./wget htt://invalid-url
```

- **Advanced examples:**

Chek test files written in shell 🙂  

## Reliable Test URLs

- https://httpbin.org/robots.txt - Small text file
- https://httpbin.org/json - JSON response
- https://httpbin.org/xml - XML response
- https://httpbin.org/html - HTML page
- https://httpbin.org/image/png - Small PNG image
- https://www.google.com/robots.txt - Google's robots.txt
- https://jsonplaceholder.typicode.com/posts/1 - JSON API
