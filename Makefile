# Makefile for dt_ping_server service installation

# Service configuration
SERVICE_NAME=dt_ping_server
SERVICE_FILE=/etc/systemd/system/$(SERVICE_NAME).service
BINARY_PATH=/usr/local/bin/$(SERVICE_NAME)

# Build the binary
build:
	go build -o $(SERVICE_NAME) .

# Install the service
install: build
	cp $(SERVICE_NAME) $(BINARY_PATH)
	echo "[Unit]\nDescription=dt_ping_server service\nAfter=network.target\n\n[Service]\nExecStart=$(BINARY_PATH)\nRestart=always\nUser=root\n\n[Install]\nWantedBy=multi-user.target" > $(SERVICE_FILE)
	systemctl daemon-reload
	systemctl enable $(SERVICE_NAME)
	systemctl start $(SERVICE_NAME)

# Uninstall the service
uninstall:
	systemctl stop $(SERVICE_NAME)
	systemctl disable $(SERVICE_NAME)
	rm -f $(BINARY_PATH)
	rm -f $(SERVICE_FILE)
	systemctl daemon-reload

# Clean up
clean:
	rm -f $(SERVICE_NAME)