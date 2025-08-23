{ pkgs, ... }:

{
  languages.go.enable = true;
  # Development environment packages
  packages = with pkgs; [
    go
    golangci-lint
    gofumpt
    gopls
    delve
    gotestsum
    go-tools
    yamllint
  ];


  # Scripts for common development tasks
  scripts = {
    gobuild.exec = "go build -o wardend .";
    gotest.exec = "go test -v ./...";
    test-coverage.exec = "go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out -o coverage.html";
    golint.exec = "golangci-lint run";
    goformat.exec = "gofumpt -w .";
    goclean.exec = "go clean && rm -f wardend coverage.out coverage.html";
  };

  # Development shell setup
  enterShell = ''
    echo "🔧 wardend development environment loaded"
    echo "Go version: $(go version)"
    echo ""
    echo "Available commands:"
    echo "  gobuild         - Build the wardend binary"
    echo "  gotest          - Run all tests"
    echo "  test-coverage - Run tests with coverage report"
    echo "  golint          - Run linter"
    echo "  goformat        - Format code"
    echo "  goclean         - Clean build artifacts"
    echo ""
  '';

  # Process composition for development
  processes = {
    # Can add file watchers or other development processes here later
  };
}
