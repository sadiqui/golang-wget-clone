#!/bin/bash

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Simple tests with guaranteed working URLs
echo "=== Simple Go Wget Tests ==="

# Build first
echo "Building..."
go build -o wget main.go

# Test 1: Basic download
echo ""
echo -e "${YELLOW}Test 1: Basic download${NC}"
./wget https://httpbin.org/robots.txt
if [ -f "robots.txt" ]; then
    echo -e "${GREEN}✓ SUCCESS - robots.txt downloaded${NC}"
    echo "Content preview:"
    head -3 robots.txt
else
    echo -e "${RED}✗ FAILED${NC}"
fi

echo ""
echo -e "${YELLOW}Test 2: Download JSON with custom name${NC}"
./wget -O test.json https://httpbin.org/json
if [ -f "test.json" ]; then
    echo -e "${GREEN}✓ SUCCESS - test.json downloaded${NC}"
    echo "Size: $(wc -c < test.json) bytes"
else
    echo -e "${RED}✗ FAILED${NC}"
fi

echo ""
echo -e "${YELLOW}Test 3: Download to directory${NC}"
mkdir -p downloads
./wget -P downloads https://httpbin.org/xml
if [ -f "downloads/xml" ]; then
    echo -e "${GREEN}✓ SUCCESS - XML downloaded to downloads/${NC}"
    echo "File exists: downloads/xml"
else
    echo -e "${RED}✗ FAILED${NC}"
fi

echo ""
echo -e "${YELLOW}Test 4: Multiple URLs${NC}"
cat > urls.txt << 'EOF'
https://example.com/index.html
https://httpbin.org/xml
EOF

./wget -i urls.txt -P batch_downloads
echo "Batch download files:"
(ls -la batch_downloads/ 2>/dev/null && echo -e "${GREEN}Files downloaded${NC}") || echo -e "${RED}No files found${NC}"

echo ""
echo -e "${YELLOW}Test 5: Rate limiting (10k)${NC}"
time ./wget --rate-limit 10k https://httpbin.org/json -O slow.json

echo ""
echo "Available test files:"
find . -name "*.txt" -o -name "*.json" -o -name "*.xml" | grep -E "(robots|test|slow)" | head -5

# Manual verification
echo ""
echo "=== Manual Tests ==="
echo "Try these working URLs manually:"
echo ""
echo "./wget https://www.google.com/robots.txt"
echo "./wget https://httpbin.org/image/png -O test.png"
echo "./wget https://jsonplaceholder.typicode.com/posts/1 -O post.json"
echo "./wget https://httpbin.org/stream/100 -O stream.txt"

# Cleanup
echo ""
read -p "Clean up test files? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    rm -f robots.txt test.json slow.json urls.txt json
    rm -rf downloads batch_downloads
    echo "Cleaned up!"
fi
