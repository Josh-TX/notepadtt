.PHONY: build frontend backend clean

build: frontend backend

frontend:
	cd frontend && npm run build

backend:
	cd backend && go build -tags embed_frontend -ldflags="-X main.version=dev" -o ../notepadtt .

clean:
	rm -rf backend/internal/webui/dist notepadtt
