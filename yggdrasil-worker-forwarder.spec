%define debug_package %{nil}

# EL7 doesn't define gobuild (it is available in go-srpm-macros which is EL8+)
%if ! 0%{?gobuild:1}
%define gobuild(o:) GO111MODULE=off go build -buildmode pie -compiler gc -tags="rpm_crashtraceback ${BUILDTAGS:-}" -ldflags "${LDFLAGS:-} -linkmode=external -B 0x$(head -c20 /dev/urandom|od -An -tx1|tr -d ' \\n') -extldflags '-Wl,-z,relro -Wl,-z,now -specs=/usr/lib/rpm/redhat/redhat-hardened-ld '" -a -v %{?**};
%endif

# The tarball ships a vendor/ directory, so build in module mode on the
# releases that default elsewhere. RHEL 10's %%gobuild defaults
# %%gomodulesmode to GO111MODULE=off, which puts go into GOPATH mode and makes
# it ignore vendor/ entirely.
%global gomodulesmode GO111MODULE=on

%global yggdrasil_worker_conf_dir %{_sysconfdir}/rhc/workers
%global dbus_name com.redhat.Yggdrasil1.Worker1.foreman_rh_cloud

Name: yggdrasil-worker-forwarder
Version: 0.1.0
Summary: Worker service for Yggdrasil that can forward requests to an API endpoint
Release: 3%{?dist}
License: GPLv3

Source0: https://github.com/theforeman/%{name}/releases/download/v%{version}/%{name}-%{version}.tar.gz
Url: https://github.com/theforeman/%{name}/

# EL7 doesn't define go_arches (it is available in go-srpm-macros which is EL8+)
%if ! 0%{?go_arches:1}
%define go_arches %{ix86} x86_64 %{arm} aarch64 ppc64le
%endif
ExclusiveArch: %{go_arches}

BuildRequires: golang
BuildRequires: go-srpm-macros
BuildRequires: go-rpm-macros
# Only needed for the %%systemd_* scriptlets below, which only run on the
# releases that get the D-Bus unit (see the %%if 0%%{?rhel} == 7 guards).
BuildRequires: systemd-rpm-macros

# yggdrasil >= 0.4 talks to its workers over D-Bus instead of the gRPC used by
# earlier releases; this package supports both from a single binary and picks
# one at startup, so either satisfies it.
Requires: (yggdrasil >= 0.2 with yggdrasil < 0.5)

%description
Worker service for Yggdrasil that forwards cloud remediation requests to the
Foreman cloud_request API. It supports both gRPC (yggdrasil < 0.4) and D-Bus
(yggdrasil >= 0.4) transports, selecting one at startup based on the presence
of the YGG_SOCKET_ADDR environment variable.

%prep
%autosetup

%build
%if 0%{?rhel} == 7
mkdir -p _gopath/src/%{name}-%{version}
cp -rf $(pwd)/*.go _gopath/src/%{name}-%{version}
cp -rf $(pwd)/vendor _gopath/src/%{name}-%{version}
export GOPATH=$(pwd)/_gopath
pushd _gopath/src/%{name}-%{version}
%{gobuild}
strip %{name}-%{version}
cp %{name}-%{version} %{_builddir}/%{name}-%{version}/%{name}
popd
%else
export GO111MODULE=on
%{gobuild} -o %{name}
strip %{name}
# EL7 never reaches a D-Bus-capable yggdrasil (stuck on 0.2.z, see README), so
# building the D-Bus policy/unit there is pure waste; EL8 is in the same boat
# but gets them anyway since the %%if above is really about macro availability,
# not yggdrasil version.
make data LIBEXECDIR=%{_libexecdir}
%endif

%install
mkdir -p %{buildroot}%{_libexecdir}
%{__install} -m 755 %{name} %{buildroot}%{_libexecdir}/%{name}
%if 0%{?rhel} != 7
install -D -d -m 755 %{buildroot}%{yggdrasil_worker_conf_dir}
install -D -m 644 _build/data/%{dbus_name}.conf %{buildroot}%{_datadir}/dbus-1/system.d/%{dbus_name}.conf
install -D -m 644 data/dbus_%{dbus_name}.service %{buildroot}%{_datadir}/dbus-1/system-services/%{dbus_name}.service
install -D -m 644 _build/data/%{dbus_name}.service %{buildroot}%{_unitdir}/%{dbus_name}.service
%endif

%files
%{_libexecdir}/%{name}
%if 0%{?rhel} != 7
%{yggdrasil_worker_conf_dir}
%{_datadir}/dbus-1/system.d/%{dbus_name}.conf
%{_datadir}/dbus-1/system-services/%{dbus_name}.service
%{_unitdir}/%{dbus_name}.service
%endif
%license LICENSE
%doc README.md

%if 0%{?rhel} != 7
%post
%systemd_post %{dbus_name}.service

%preun
%systemd_preun %{dbus_name}.service

%postun
%systemd_postun_with_restart %{dbus_name}.service
%endif

%changelog
* Thu Oct 01 2026 Lucy Fu <lufu@redhat.com> - 0.1.0
- Add D-Bus transport support for yggdrasil >= 0.4 (RHEL 10)

* Tue Sep 08 2026 Odilon Sousa <osousa@redhat.com> - 0.1.0-2
- Rebuild against new golang version

* Fri Jul 31 2026 Zach Huntington-Meath <zhunting@redhat.com> - 0.1.0-1
- Release yggdrasil-worker-forwarder 0.1.0

* Thu Jul 02 2026 Eric D. Helms <ericdhelms@gmail.com> - 0.0.4-1
- Release yggdrasil-worker-forwarder 0.0.4

* Mon Oct 16 2023 Eric D. Helms <ericdhelms@gmail.com> - 0.0.3-1
- Release 0.0.3

* Tue Apr 05 2022 Eric D. Helms <ericdhelms@gmail.com> - 0.0.1-1
- Release 0.0.1
