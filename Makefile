build:
	go build -ldflags="-s -w" -o carbonio-files-client cmd/carbonio-files-go-client/main.go

test:
	go test ./...