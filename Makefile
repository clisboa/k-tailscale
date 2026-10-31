QT := Qt5Core Qt5Gui Qt5Widgets Qt5Network
QR_SOURCES = src/qr_code.cc src/qr_widget.cc src/vendor/qrcodegen.cc
QR_HEADERS = src/qr_license.h src/qr_code.h src/qr_widget.h src/vendor/qrcodegen.hpp
HOST_CXX ?= g++
HOST_FLAGS = -std=c++11 -fPIC -Wall -Wextra -Werror -Isrc -Ibuild $(shell pkg-config --cflags $(QT))
HOST_LIBS = $(shell pkg-config --libs $(QT))
CROSS ?= arm-nickel-linux-gnueabihf-
ARM_FLAGS = -O2 -std=gnu++11 -fPIC -fvisibility=hidden -Wall -Wextra -Werror -march=armv7-a -mtune=cortex-a8 -mfpu=neon -mfloat-abi=hard -mthumb -Isrc $(shell $(CROSS)pkg-config --cflags $(QT))
ARM_LIBS = $(shell $(CROSS)pkg-config --libs $(QT)) -ldl
ARM_SOURCES = $(wildcard src/*.cc) src/vendor/qrcodegen.cc
ARM_OBJECTS = $(ARM_SOURCES:src/%.cc=build/arm/%.o)
ARM_MOC = $(if $(wildcard src/image_plugin.h),build/arm/image_plugin.moc.o)

.PHONY: test test-core test-actions test-client test-panel test-recovery test-injector test-widget test-qr
test: test-qr test-core test-actions test-client test-panel test-recovery test-injector test-widget

build:
	mkdir -p build

build/test_widget: tests/test_widget.cc src/status_glyph.cc src/status_glyph.h src/popup_geometry.cc src/popup_geometry.h | build
	$(HOST_CXX) $(HOST_FLAGS) tests/test_widget.cc src/status_glyph.cc src/popup_geometry.cc -o $@ $(HOST_LIBS)

test-widget: build/test_widget
	QT_QPA_PLATFORM=offscreen ./build/test_widget

build/test_core: tests/test_core.cc src/model.cc src/model.h | build
	$(HOST_CXX) $(HOST_FLAGS) tests/test_core.cc src/model.cc -o $@ $(HOST_LIBS)

test-core: build/test_core
	./build/test_core

build/test_actions: tests/test_actions.cc src/actions.cc src/actions.h | build
	$(HOST_CXX) $(HOST_FLAGS) tests/test_actions.cc src/actions.cc -o $@ $(HOST_LIBS)

test-actions: build/test_actions
	./build/test_actions

build/test_client: tests/test_client.cc src/client.cc src/client.h src/model.cc src/model.h | build
	$(HOST_CXX) $(HOST_FLAGS) tests/test_client.cc src/client.cc src/model.cc -o $@ $(HOST_LIBS)

test-client: build/test_client
	./build/test_client

build/test_panel: tests/test_panel.cc src/panel.cc $(QR_SOURCES) $(QR_HEADERS) src/panel.h src/popup_geometry.cc src/button_icons.cc src/touch_button.h src/button_icons.h src/controller.cc src/updates.cc src/client.cc src/actions.cc src/model.cc src/log.cc | build
	$(HOST_CXX) $(HOST_FLAGS) tests/test_panel.cc src/panel.cc $(QR_SOURCES) src/popup_geometry.cc src/button_icons.cc src/controller.cc src/updates.cc src/client.cc src/actions.cc src/model.cc src/log.cc -o $@ $(HOST_LIBS)

test-panel: build/test_panel
	QT_QPA_PLATFORM=offscreen ./build/test_panel
	python3 tests/decode_qr.py panel

build/test_recovery: tests/test_recovery.cc src/recovery.cc src/log.cc | build
	$(HOST_CXX) $(HOST_FLAGS) tests/test_recovery.cc src/recovery.cc src/log.cc -o $@ $(HOST_LIBS)

test-recovery: build/test_recovery
	./build/test_recovery

build/test_injector.moc: tests/test_injector.cc | build
	moc $< -o $@

build/test_injector: tests/test_injector.cc build/test_injector.moc src/injector.cc src/status_glyph.cc src/panel.cc $(QR_SOURCES) $(QR_HEADERS) src/panel.h src/popup_geometry.cc src/button_icons.cc src/touch_button.h src/button_icons.h src/controller.cc src/updates.cc src/client.cc src/actions.cc src/model.cc src/log.cc | build
	$(HOST_CXX) $(HOST_FLAGS) tests/test_injector.cc src/injector.cc src/status_glyph.cc src/panel.cc $(QR_SOURCES) src/popup_geometry.cc src/button_icons.cc src/controller.cc src/updates.cc src/client.cc src/actions.cc src/model.cc src/log.cc -o $@ $(HOST_LIBS)

test-injector: build/test_injector
	QT_QPA_PLATFORM=offscreen ./build/test_injector

.PHONY: arm
arm: build/libtailscaleui.so

build/arm:
	mkdir -p build/arm

# GCC 4.9 misdiagnoses std::array's standard {} zero-initialization in the
# unmodified vendor source. Keep all other warnings and application checks.
build/arm/vendor/qrcodegen.o: ARM_FLAGS += -Wno-missing-field-initializers

build/arm/%.o: src/%.cc $(wildcard src/*.h) $(wildcard src/vendor/*.hpp) | build/arm
	mkdir -p $(dir $@)
	$(CROSS)g++ $(ARM_FLAGS) -c $< -o $@

build/arm/image_plugin.moc.cc: src/image_plugin.h src/image_plugin.json | build/arm
	moc $< -o $@

build/arm/image_plugin.moc.o: build/arm/image_plugin.moc.cc
	mkdir -p $(dir $@)
	$(CROSS)g++ $(ARM_FLAGS) -c $< -o $@

build/libtailscaleui.so: $(ARM_OBJECTS) $(ARM_MOC)
	$(CROSS)g++ -shared -Wl,--no-undefined -Wl,--as-needed -pthread -o $@ $^ $(ARM_LIBS)
	$(CROSS)strip --strip-unneeded $@

build/test_qr: tests/test_qr.cc $(QR_SOURCES) $(QR_HEADERS) | build
	$(HOST_CXX) $(HOST_FLAGS) tests/test_qr.cc $(QR_SOURCES) -o $@ $(HOST_LIBS)

test-qr: build/test_qr
	QT_QPA_PLATFORM=offscreen ./build/test_qr
	python3 tests/decode_qr.py
