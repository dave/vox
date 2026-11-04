BIN := vox
PREFIX := $(HOME)/.local/bin

build:
	go build -ldflags "-extldflags '-sectcreate __TEXT __info_plist $(CURDIR)/Info.plist'" -o $(BIN) .
	codesign -s - -f -i com.dave.vox $(BIN)

install: build
	mkdir -p $(PREFIX) && cp $(BIN) $(PREFIX)/$(BIN)
	codesign -s - -f -i com.dave.vox $(PREFIX)/$(BIN)

.PHONY: build install

AGENT := $(HOME)/Library/LaunchAgents/com.dave.vox.plist

# Run vox at login via launchd (logs to ~/Library/Logs/vox.log).
agent: install
	mkdir -p $(HOME)/Library/LaunchAgents
	sed 's|__HOME__|$(HOME)|g' launchd/com.dave.vox.plist > $(AGENT)
	launchctl bootout gui/$$(id -u)/com.dave.vox 2>/dev/null || true
	launchctl bootstrap gui/$$(id -u) $(AGENT)

unagent:
	launchctl bootout gui/$$(id -u)/com.dave.vox 2>/dev/null || true
	rm -f $(AGENT)

restart:
	launchctl kickstart -k gui/$$(id -u)/com.dave.vox

.PHONY: agent unagent restart
