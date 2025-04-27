#!/bin/bash
set -x
set -e

# Detect OS and choose grep command
if [[ "$OSTYPE" == "darwin"* ]]; then
    # macOS - Add Homebrew paths to PATH
    if [ -d "/opt/homebrew/bin" ]; then
        export PATH="/opt/homebrew/bin:$PATH"  # Apple Silicon
    elif [ -d "/usr/local/bin" ]; then
        export PATH="/usr/local/bin:$PATH"     # Intel Mac
    fi
    
    # Check if ggrep is available after PATH update
    GREP_CMD="ggrep"
    if ! command -v $GREP_CMD &> /dev/null; then
        if command -v brew &> /dev/null; then
            echo "Error: ggrep not found. Please install GNU grep: brew install grep"
        else
            echo "Error: Homebrew not found in PATH. Please install Homebrew or GNU grep manually."
        fi
        exit 1
    fi
else
    # Linux and others
    GREP_CMD="grep"
fi

found_chinese=false
while IFS= read -r line; do
    if [ -n "$line" ]; then
        echo "Chinese chars found in $line"
        found_chinese=true
    fi
done < <(find . \( -name "*.go" -o -name "*.cpp" -o -name "*.h" -o -name "*.hpp" \
    -o -name "*.sol" -o -name "*.js" -o -name "*.jsx" -o -name "*.ts" \
    -o -name "*.tsx" -o -name "Makefile" -o -name "*.mk" \
    -o -name "Dockerfile" -o -name "*.md" \
    -o -name "*.yaml" -o -name "*.yml" \) \
    -type f -exec sh -c "$GREP_CMD -H -n -P \"[\x{4e00}-\x{9fa5}]\" \"\$0\"" {} \;)
if [ "$found_chinese" = true ]; then
    echo "Error: Found Chinese characters in source files!"
    exit 1
fi