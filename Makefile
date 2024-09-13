# Define build directory
BUILD_DIR=build

# Define binaries output names
BINARY_LINUX=$(BUILD_DIR)/podminator-linux-amd64
BINARY_WINDOWS=$(BUILD_DIR)/podminator-windows-amd64.exe
BINARY_MAC=$(BUILD_DIR)/podminator-darwin-amd64

# Set the Go environment for building on different platforms
GOOS_LINUX=linux
GOOS_WINDOWS=windows
GOOS_DARWIN=darwin
GOARCH=amd64

.PHONY: all linux windows mac clean

# Default target: build all binaries
all: linux windows mac

# Build for Linux
linux:
	@echo "Building for Linux..."
	GOOS=$(GOOS_LINUX) GOARCH=$(GOARCH) go build -o $(BINARY_LINUX)

# Build for Windows
windows:
	@echo "Building for Windows..."
	GOOS=$(GOOS_WINDOWS) GOARCH=$(GOARCH) go build -o $(BINARY_WINDOWS)

# Build for macOS
mac:
	@echo "Building for macOS..."
	GOOS=$(GOOS_DARWIN) GOARCH=$(GOARCH) go build -o $(BINARY_MAC)

# Clean up the build directory
clean:
	@echo "Cleaning up..."
	rm -rf $(BUILD_DIR)

# Ensure build directory exists before building
$(BUILD_DIR):
	@mkdir -p $(BUILD_DIR)
