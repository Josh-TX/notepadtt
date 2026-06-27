.PHONY: build frontend backend clean

build: frontend backend

frontend:
	cd frontend && npm run build

backend:
	go build -o notepadtt .

clean:
	rm -rf frontend/dist notepadtt
