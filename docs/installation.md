[← Back to README](../README.md)

## Installation

### Build from source

```bash
make build
```

This installs the frontend's npm dependencies, builds it (Svelte + Tailwind CSS, see [Desktop GUI frontend](desktop-gui.md#desktop-gui-frontend)) into `cmd/carbonio-files-go-client/frontend/dist/`, then builds the Go binary, which embeds that output. It produces the `CarbonioFileSync` binary in the project root (symbols stripped and optimized).
