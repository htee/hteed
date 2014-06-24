GO ?= go

PKG_LIST := server client
BUILDDIR := bin

all: clean build

clean: $(BUILDDIR)
	rm -f $(BUILDDIR)/*

build: $(PKG_LIST)

$(PKG_LIST):
	pushd $(BUILDDIR) ; $(GO) build ../htee-$@/* ; popd
