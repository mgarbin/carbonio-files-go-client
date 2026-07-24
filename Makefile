FRONTEND_DIR := cmd/carbonio-files-go-client/frontend

.PHONY: build build-macos test frontend

frontend:
	npm install --prefix $(FRONTEND_DIR)
	npm run build --prefix $(FRONTEND_DIR)

build: frontend
	CGO_CFLAGS="$$CGO_CFLAGS -Wno-deprecated-declarations" go build -tags "webkit2_41,production" -ldflags="-s -w" -o carbonio-files-client ./cmd/carbonio-files-go-client

build-macos: frontend
	CGO_CFLAGS="$$CGO_CFLAGS -Wno-deprecated-declarations" \
	CGO_LDFLAGS="$$CGO_LDFLAGS -framework UniformTypeIdentifiers" \
	go build -tags "webkit2_41,production" -ldflags="-s -w" -o carbonio-files-client ./cmd/carbonio-files-go-client

test: frontend
	go test ./...