.PHONY: native standalone verify test build compat

native:
	bash ./scripts/build-native.sh

standalone:
	bash ./scripts/build-standalone.sh

verify:
	bash ./scripts/verify-standalone.sh ./dist/hitvid-linux-amd64

test:
	go test ./...

build: standalone

compat:
	go build -o hitvid-compat .
