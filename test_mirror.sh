#!/bin/bash

echo "Building the project..."
go build -o wget .
if [ $? -ne 0 ]; then
    echo "Build failed. Aborting tests."
    exit 1
fi
echo "Build succeeded."

# Test basic mirroring
./wget --mirror https://httpbin.org
[ -d "httpbin.org" ] && echo "PASS: Basic mirror" || echo "FAIL: Basic mirror"

# Test -R rejection
./wget --mirror -R=png,jpg https://httpbin.org
find httpbin.org -name "*.png" | grep -q . && echo "FAIL: -R" || echo "PASS: -R"

# Test -X exclusion
./wget --mirror -X=/anything,/html https://httpbin.org
[ ! -d "httpbin.org/anything" ] && echo "PASS: -X" || echo "FAIL: -X"

# Test --convert-links (manual verification needed)
./wget --mirror --convert-links https://httpbin.org
echo "Open httpbin.org/robots.txt to check if links are relative"