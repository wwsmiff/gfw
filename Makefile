gfw: main.go go.mod
	go build .

.PHONY: run
run:
	go run . --build "go build ." --exec "./testserver" --root "./testserver"

clean:
	rm ./gfw
