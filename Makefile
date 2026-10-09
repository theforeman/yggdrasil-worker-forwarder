PKGNAME := yggdrasil-worker-forwarder
LIBEXECDIR := /usr/libexec
WORKER_GROUP := yggdrasil-worker

ifeq ($(origin VERSION), undefined)
	VERSION := 0.2.0
endif

.PHONY: build
build: submodule
	mkdir -p _build
	CGO_ENABLED=0 go build -o _build/$(PKGNAME) .

# The gRPC protocol package lives in the yggdrasil submodule; without it the
# build fails with a confusing module resolution error.
.PHONY: submodule
submodule:
	@test -f yggdrasil/go.mod || git submodule update --init

.PHONY: data
data: _build/data/com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.conf _build/data/com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.service

.PHONY: install
install: build data
	install -D -m 755 _build/$(PKGNAME) $(DESTDIR)$(LIBEXECDIR)/$(PKGNAME)
	install -D -m 644 _build/data/com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.conf $(DESTDIR)/usr/share/dbus-1/system.d/com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.conf
	install -D -m 644 data/dbus_com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.service $(DESTDIR)/usr/share/dbus-1/system-services/com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.service
	install -D -m 644 _build/data/com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.service $(DESTDIR)/usr/lib/systemd/system/com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud.service

_build/data/%: data/%.in
	mkdir -p $(@D)
	sed \
		-e 's,[@]libexecdir[@],$(LIBEXECDIR),g' \
		-e 's,[@]worker_group[@],$(WORKER_GROUP),g' \
		-e 's,[@]executable[@],$(PKGNAME),g' \
		$< > $@

clean:
	rm -rf _build

distribution-tarball: submodule
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

test: submodule
	go test *.go

vet: submodule
	go vet *.go
