#!/bin/bash

echo "=== Large File Progress Bar Test ==="
echo ""

# Build the wget tool
echo "Building wget..."
go build -o wget main.go

echo ""
echo "Choose a test file size:"
echo "1) 100MB file (good for testing, ~1-2 minutes)"
echo "2) 1GB file (longer test, ~5-10 minutes)"
echo "3) 10GB file (very long test, can interrupt with Ctrl+C)"
echo ""
read -p "Enter choice (1-3): " choice

case $choice in
    1)
        echo ""
        echo "Downloading 100MB test file..."
        ./wget -O test_100mb.bin https://ash-speed.hetzner.com/100MB.bin
        FILE="test_100mb.bin"
        ;;
    2)
        echo ""
        echo "Downloading 1GB test file (this will take several minutes)..."
        ./wget -O test_1gb.bin https://ash-speed.hetzner.com/1GB.bin
        FILE="test_1gb.bin"
        ;;
    3)
        echo ""
        echo "Downloading 10GB test file (press Ctrl+C when you've seen enough)..."
        ./wget -O test_10gb.bin https://ash-speed.hetzner.com/10GB.bin
        FILE="test_10gb.bin"
        ;;
    *)
        echo "Invalid choice. Using 100MB file as default."
        ./wget -O test_100mb.bin https://ash-speed.hetzner.com/100MB.bin
        FILE="test_100mb.bin"
        ;;
esac

echo ""
if [ -f "$FILE" ]; then
    echo "✓ SUCCESS - File downloaded successfully"
    echo "File size: $(ls -lh $FILE | awk '{print $5}')"
    
    # Clean up
    echo ""
    read -p "Delete the test file? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        rm "$FILE"
        echo "Test file deleted."
    else
        echo "Test file kept: $FILE"
    fi
else
    echo "✗ Download failed or was interrupted"
fi