#pragma once
#include <QIcon>

namespace tailui {
enum class ButtonGlyph { Connect, Disconnect, SignOut, Refresh, Close, Preparing, Back, Up, Down };
QSize buttonIconSize(int edge);
QIcon buttonIcon(ButtonGlyph glyph, int size, bool pressed = false);
} // namespace tailui
