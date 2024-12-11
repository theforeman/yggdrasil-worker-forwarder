PKGNAME := yggdrasil-worker-forwarder
PKGNAME_DBUS := foreman_rh_cloud_forwarder_dbus
LIBEXECDIR := /usr/libexec
WORKER_GROUP := yggdrasil-worker

ifeq ($(origin VERSION), undefined)
	VERSION := 0.0.3
endif

.PHONY: build
build:
	CGO_ENABLED=0 go build -o build/$(PKGNAME) main.go server.go
	CGO_ENABLED=0 go build -o build/$(PKGNAME_DBUS) dbus/*.go


.PHONY: data
data: build/data/com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.conf build/data/com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.service

.PHONY: install
install: build data
	install -D -m 755 build/$(PKGNAME) $(DESTDIR)$(LIBEXECDIR)/$(PKGNAME)
	install -D -m 755 build/$(PKGNAME_DBUS) $(DESTDIR)$(LIBEXECDIR)/$(PKGNAME_DBUS)
	install -D -m 644 build/data/com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.conf $(DESTDIR)/usr/share/dbus-1/system.d/com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.conf
	install -D -m 644 dbus/data/dbus_com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.service $(DESTDIR)/usr/share/dbus-1/system-services/com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.service
	install -D -m 644 build/data/com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.service $(DESTDIR)/usr/lib/systemd/system/com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.service

build/data/%: dbus/data/%.in
	mkdir -p $(@D)
	sed \
		-e 's,[@]libexecdir[@],$(LIBEXECDIR),g' \
		-e 's,[@]worker_group[@],$(WORKER_GROUP),g' \
		-e 's,[@]executable[@],$(PKGNAME_DBUS),g' \
		$< > $@


clean:
	rm -rf build

distribution-tarball:
	go mod vendor
	tar --create \
		--gzip \
		--file /tmp/$(PKGNAME)-$(VERSION).tar.gz \
		--exclude=.git \
		--exclude=.vscode \
		--exclude=.github \
		--exclude=.gitignore \
		--exclude=.copr \
		--transform s/^\./$(PKGNAME)-$(VERSION)/ \
		. && mv /tmp/$(PKGNAME)-$(VERSION).tar.gz .
	rm -rf ./vendor

test:
	go test *.go

vet:
	go vet *.go
