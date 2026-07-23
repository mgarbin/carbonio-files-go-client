FRONTEND_DIR := cmd/carbonio-files-go-client/frontend

.PHONY: build test frontend

frontend:
	npm install --prefix $(FRONTEND_DIR)
	npm run build --prefix $(FRONTEND_DIR)

build: frontend
	go build -tags "webkit2_41,production" -ldflags="-s -w" -o carbonio-files-client ./cmd/carbonio-files-go-client

test: frontend
	go test ./...