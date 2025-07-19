#!/bin/bash

# Test script for Go wget clone
# Make sure to build the binary first: go build -o wget main.go

echo "=== Go Wget Clone Test Suite ==="
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to run test and check result
run_test() {
    local test_name="$1"
    local command="$2"
    local expected_file="$3"
    
    echo -e "${YELLOW}Testing: $test_name${NC}"
    echo "Command: $command"
    echo ""
    
    # Clean up before test
    if [ -n "$expected_file" ] && [ -f "$expected_file" ]; then
        rm "$expected_file"
    fi
    
    # Run the command
    if eval "$command"; then
        # Check if expected file exists
        if [ -n "$expected_file" ]; then
            if [ -f "$expected_file" ]; then
                echo -e "${GREEN}✓ SUCCESS: File downloaded successfully${NC}"
                echo "File size: $(du -h "$expected_file" | cut -f1)"
            else
                echo -e "${RED}✗ FAILED: Expected file not found${NC}"
                return 1
            fi
        else
            echo -e "${GREEN}✓ SUCCESS: Command executed without error${NC}"
        fi
    else
        echo -e "${RED}✗ FAILED: Command failed${NC}"
        return 1
    fi
    echo "----------------------------------------"
    echo ""
    return 0
}

# Create test directories
mkdir -p test_downloads
mkdir -p test_mirror

echo "Building wget binary..."
if ! go build -o wget main.go; then
    echo -e "${RED}Failed to build binary${NC}"
    exit 1
fi
echo ""

# Test 1: Basic download - Small text file
run_test "Basic Download (Small Text File)" \
    "./wget https://httpbin.org/robots.txt" \
    "robots.txt"

# Test 2: Download with custom filename
run_test "Custom Filename (-O flag)" \
    "./wget -O test_robots.txt https://httpbin.org/robots.txt" \
    "test_robots.txt"

# Test 3: Download to directory
run_test "Download to Directory (-P flag)" \
    "./wget -P test_downloads https://httpbin.org/robots.txt" \
    "test_downloads/robots.txt"

# Test 4: Download JSON data
run_test "Download JSON Data" \
    "./wget -O test_json.json https://httpbin.org/json" \
    "test_json.json"

# Test 5: Rate limited download
run_test "Rate Limited Download (50k limit)" \
    "./wget --rate-limit 50k -O rate_limited.json https://httpbin.org/json" \
    "rate_limited.json"

# Test 6: Create input file for multiple downloads
echo "Creating input file for multiple downloads..."
cat > urls.txt << EOF
https://httpbin.org/robots.txt
https://httpbin.org/json
https://httpbin.org/xml
EOF

# Test 7: Multiple file download
run_test "Multiple File Download (-i flag)" \
    "./wget -i urls.txt -P test_downloads" \
    ""

# Check if all files from multiple download exist
echo "Checking multiple download results..."
files=("test_downloads/robots.txt" "test_downloads/json" "test_downloads/xml")
all_exist=true
for file in "${files[@]}"; do
    if [ -f "$file" ]; then
        echo -e "${GREEN}✓ Found: $file${NC}"
    else
        echo -e "${RED}✗ Missing: $file${NC}"
        all_exist=false
    fi
done

if [ "$all_exist" = true ]; then
    echo -e "${GREEN}✓ Multiple download test PASSED${NC}"
else
    echo -e "${RED}✗ Multiple download test FAILED${NC}"
fi
echo ""

# Test 8: Background download test (we'll just start it and check log)
echo -e "${YELLOW}Testing: Background Download${NC}"
echo "Command: ./wget -B https://httpbin.org/delay/2"
echo ""

./wget -B https://httpbin.org/delay/2
sleep 3
if [ -f "wget-log" ]; then
    echo -e "${GREEN}✓ Background download started successfully${NC}"
    echo "Log file created:"
    head -5 wget-log
else
    echo -e "${RED}✗ Background download failed${NC}"
fi
echo "----------------------------------------"
echo ""

# Test 9: Error handling - 404 test
echo -e "${YELLOW}Testing: Error Handling (404)${NC}"
echo "Command: ./wget https://httpbin.org/status/404"
echo ""

if ./wget https://httpbin.org/status/404 2>&1 | grep -q "404"; then
    echo -e "${GREEN}✓ SUCCESS: 404 error handled correctly${NC}"
else
    echo -e "${RED}✗ FAILED: 404 error not handled properly${NC}"
fi
echo "----------------------------------------"
echo ""

# Test 10: Simple mirror test (limited to avoid being too aggressive)
echo -e "${YELLOW}Testing: Basic Mirror Functionality${NC}"
echo "Command: ./wget --mirror https://httpbin.org/robots.txt"
echo ""

cd test_mirror
if ../wget --mirror https://httpbin.org/robots.txt; then
    if [ -f "robots.txt" ] || [ -f "index.html" ]; then
        echo -e "${GREEN}✓ SUCCESS: Mirror created files${NC}"
        ls -la
    else
        echo -e "${RED}✗ FAILED: No mirror files created${NC}"
    fi
else
    echo -e "${RED}✗ FAILED: Mirror command failed${NC}"
fi
cd ..
echo "----------------------------------------"
echo ""

# Summary
echo "=== Test Summary ==="
echo ""
echo "Files created during testing:"
find . -name "*.txt" -o -name "*.json" -o -name "*.xml" -o -name "wget-log" | head -10

echo ""
echo "Test directories:"
echo "- test_downloads/: $(ls test_downloads 2>/dev/null | wc -l) files"
echo "- test_mirror/: $(ls test_mirror 2>/dev/null | wc -l) files"

echo ""
echo "=== Manual Verification Commands ==="
echo "1. Check a downloaded file:"
echo "   cat robots.txt"
echo ""
echo "2. Verify JSON download:"
echo "   cat test_json.json | head -3"
echo ""
echo "3. Check directory contents:"
echo "   ls -la test_downloads/"
echo ""
echo "4. Test with a real website (be careful):"
echo "   ./wget https://www.google.com/robots.txt"
echo ""

# Cleanup option
echo "=== Cleanup ==="
read -p "Do you want to clean up test files? (y/n): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    rm -f robots.txt test_robots.txt test_json.json rate_limited.json urls.txt wget-log
    rm -rf test_downloads test_mirror
    echo "Test files cleaned up."
fi

echo ""
echo "=== Additional Real-World Tests ==="
echo "Try these commands for real testing:"
echo ""
echo "# Download a small image"
echo "./wget https://httpbin.org/image/png -O test_image.png"
echo ""
echo "# Download with User-Agent verification"
echo "./wget https://httpbin.org/user-agent -O user_agent_test.json"
echo ""
echo "# Test larger file with progress"
echo "./wget https://httpbin.org/stream/1000 -O stream_test.txt"
echo ""
echo "# Test redirect handling"
echo "./wget https://httpbin.org/redirect/1 -O redirect_test.html"
