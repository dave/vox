BIN := vox
PREFIX := $(HOME)/.local/bin

build:
	go build -ldflags "-extldflags '-sectcreate __TEXT __info_plist $(CURDIR)/Info.plist'" -o $(BIN) .
	codesign -s - -f -i com.dave.vox $(BIN)

install: build
	mkdir -p $(PREFIX) && cp $(BIN) $(PREFIX)/$(BIN)
	codesign -s - -f -i com.dave.vox $(PREFIX)/$(BIN)

.PHONY: build install
