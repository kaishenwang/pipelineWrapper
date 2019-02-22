all: wrapper

clean:
	rm  -f  wrapper

wrapper:
	go build
	cd $$GOPATH/src/github.com/kwang40/zdns && make

.PHONY:
	wrapper clean
