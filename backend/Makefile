.PHONY: build run test clean deps fmt lint build-all build-windows build-linux build-darwin

# 编译输出目录
BUILD_DIR := build
BINARY_NAME := doc-server

# Go编译参数 - 禁用CGO，纯Go实现
GOFLAGS := -ldflags "-s -w"
CGO_ENABLED := 0

# CGO编译参数（仅在需要时启用）
CGO_GOFLAGS := -ldflags "-s -w"
CGO_CGO_ENABLED := 1

build:
	@echo "Building for current platform (CGO disabled)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) main.go

build-cgo:
	@echo "Building with CGO enabled..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=$(CGO_CGO_ENABLED) go build $(CGO_GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-cgo main.go

build-dev:
	@echo "Building for development..."
	CGO_ENABLED=$(CGO_ENABLED) go build -o $(BUILD_DIR)/$(BINARY_NAME)-dev main.go

# Windows x64
build-windows:
	@echo "Cross-compiling for Windows x64..."
	@mkdir -p $(BUILD_DIR)/windows
	GOOS=windows GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -o $(BUILD_DIR)/windows/$(BINARY_NAME).exe main.go
	@echo "Built: $(BUILD_DIR)/windows/$(BINARY_NAME).exe"

# Windows x86
build-windows-386:
	@echo "Cross-compiling for Windows x86..."
	@mkdir -p $(BUILD_DIR)/windows-386
	GOOS=windows GOARCH=386 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -o $(BUILD_DIR)/windows-386/$(BINARY_NAME).exe main.go
	@echo "Built: $(BUILD_DIR)/windows-386/$(BINARY_NAME).exe"

# Linux x64
build-linux:
	@echo "Cross-compiling for Linux x64..."
	@mkdir -p $(BUILD_DIR)/linux
	GOOS=linux GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -o $(BUILD_DIR)/linux/$(BINARY_NAME) main.go
	@echo "Built: $(BUILD_DIR)/linux/$(BINARY_NAME)"

# Linux ARM64
build-linux-arm64:
	@echo "Cross-compiling for Linux ARM64..."
	@mkdir -p $(BUILD_DIR)/linux-arm64
	GOOS=linux GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -o $(BUILD_DIR)/linux-arm64/$(BINARY_NAME) main.go
	@echo "Built: $(BUILD_DIR)/linux-arm64/$(BINARY_NAME)"

# Linux ARMv7
build-linux-arm:
	@echo "Cross-compiling for Linux ARMv7..."
	@mkdir -p $(BUILD_DIR)/linux-arm
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -o $(BUILD_DIR)/linux-arm/$(BINARY_NAME) main.go
	@echo "Built: $(BUILD_DIR)/linux-arm/$(BINARY_NAME)"

# macOS x64
build-darwin:
	@echo "Cross-compiling for macOS x64..."
	@mkdir -p $(BUILD_DIR)/darwin
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -o $(BUILD_DIR)/darwin/$(BINARY_NAME) main.go
	@echo "Built: $(BUILD_DIR)/darwin/$(BINARY_NAME)"

# macOS ARM64 (Apple Silicon)
build-darwin-arm64:
	@echo "Cross-compiling for macOS ARM64..."
	@mkdir -p $(BUILD_DIR)/darwin-arm64
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=$(CGO_ENABLED) go build $(GOFLAGS) -o $(BUILD_DIR)/darwin-arm64/$(BINARY_NAME) main.go
	@echo "Built: $(BUILD_DIR)/darwin-arm64/$(BINARY_NAME)"

# 编译所有平台
build-all: build-windows build-linux build-darwin build-linux-arm64
	@echo "========================================="
	@echo "All platforms built successfully!"
	@echo "========================================="
	@ls -lh $(BUILD_DIR)/*/$(BINARY_NAME)* 2>/dev/null || true

run:
	CGO_ENABLED=$(CGO_ENABLED) go run main.go

test:
	go test -v ./...

clean:
	rm -rf $(BUILD_DIR)
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_NAME)-dev.exe

deps:
	go mod download
	go mod tidy

fmt:
	go fmt ./...

lint:
	golangci-lint run

dev:
	CGO_ENABLED=$(CGO_ENABLED) go run main.go

install:
	go mod download
	CGO_ENABLED=$(CGO_ENABLED) go build -o $(BINARY_NAME) main.go

# Docker targets
docker-build:
	docker build -t doc-server .

docker-build-multiple:
	@echo "Building Docker images for multiple platforms..."
	docker buildx create --name mybuilder || true
	docker buildx use mybuilder
	docker buildx build --platform linux/amd64,linux/arm64 -t doc-server:latest --push .

docker-run:
	docker run -p 8080:8080 -v $(PWD)/data:/app/data doc-server

# 信息输出
info:
	@echo "Go Version:" && go version
	@echo "GOOS:" && echo $(GOOS)
	@echo "GOARCH:" && echo $(GOARCH)
	@echo "CGO_ENABLED:" && echo $(CGO_ENABLED)

# 压缩发布的二进制文件
package-windows:
	@mkdir -p $(BUILD_DIR)/release
	cd $(BUILD_DIR)/windows && zip -r ../release/$(BINARY_NAME)-windows-amd64.zip $(BINARY_NAME).exe

package-linux:
	@mkdir -p $(BUILD_DIR)/release
	tar -czf $(BUILD_DIR)/release/$(BINARY_NAME)-linux-amd64.tar.gz -C $(BUILD_DIR)/linux $(BINARY_NAME)
	chmod +x $(BUILD_DIR)/linux/$(BINARY_NAME)

package-all: package-windows package-linux
	@echo "Packages created in $(BUILD_DIR)/release/"
	@ls -lh $(BUILD_DIR)/release/

# 帮助
help:
	@echo "Yanglao Server Build System"
	@echo ""
	@echo "Features:"
	@echo "  - Pure Go implementation (no CGO required)"
	@echo "  - Single binary deployment"
	@echo "  - Cross-platform compilation"
	@echo ""
	@echo "Available targets:"
	@echo "  build            - Build for current platform (CGO disabled)"
	@echo "  build-cgo        - Build with CGO enabled (uses mattn/go-sqlite3)"
	@echo "  build-dev        - Build for development"
	@echo "  build-windows    - Cross-compile for Windows x64"
	@echo "  build-windows-386 - Cross-compile for Windows x86"
	@echo "  build-linux      - Cross-compile for Linux x64"
	@echo "  build-linux-arm64 - Cross-compile for Linux ARM64"
	@echo "  build-linux-arm  - Cross-compile for Linux ARMv7"
	@echo "  build-darwin     - Cross-compile for macOS x64"
	@echo "  build-darwin-arm64 - Cross-compile for macOS ARM64"
	@echo "  build-all        - Build for all platforms"
	@echo "  package-windows   - Create Windows zip package"
	@echo "  package-linux     - Create Linux tar.gz package"
	@echo "  package-all       - Create packages for all platforms"
	@echo "  run              - Run the application"
	@echo "  test             - Run tests"
	@echo "  clean            - Clean build artifacts"
	@echo "  deps             - Download dependencies"
	@echo "  docker-build     - Build Docker image"
	@echo "  info             - Show build environment info"
	@echo "  help             - Show this help message"
