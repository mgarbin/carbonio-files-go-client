build:
	go build -tags "webkit2_41,production" -ldflags="-s -w" -o carbonio-files-client ./cmd/carbonio-files-go-client

test:
	go test ./...