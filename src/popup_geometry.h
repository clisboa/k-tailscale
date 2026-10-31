#pragma once
#include <QRect>
#include <QSize>
namespace tailui {
struct PopupPlacement { QRect frame; int tipX=0; };
PopupPlacement placePopup(const QRect &window,const QRect &anchor,const QSize &wanted,int margin,int arrow);
}
