#include "button_icons.h"
#include <QPainter>
#include <QPainterPath>
#include <QPixmap>

namespace tailui {
namespace {
QPixmap drawGlyph(ButtonGlyph glyph, int size, const QColor &color) {
    QPixmap pixmap(buttonIconSize(size)); pixmap.fill(Qt::transparent);
    QPainter p(&pixmap); p.setRenderHint(QPainter::Antialiasing);
    p.scale(size/24.0,size/24.0);
    p.setPen(QPen(color,1.9,Qt::SolidLine,Qt::RoundCap,Qt::RoundJoin));
    p.setBrush(Qt::NoBrush);
    QPainterPath path;
    switch(glyph) {
    case ButtonGlyph::Connect:
    case ButtonGlyph::Disconnect: {
        p.translate(12,12); p.rotate(-35); p.translate(-12,-12);
        const bool joined=glyph==ButtonGlyph::Connect;
        const qreal left=joined?6:5, right=joined?18:19;
        path.moveTo(joined?9:7,8); path.lineTo(left,8);
        path.cubicTo(left-5.333,8,left-5.333,16,left,16); path.lineTo(joined?9:7,16);
        path.moveTo(joined?15:17,8); path.lineTo(right,8);
        path.cubicTo(right+5.333,8,right+5.333,16,right,16); path.lineTo(joined?15:17,16);
        if(joined) { path.moveTo(8,12); path.lineTo(16,12); }
        else { path.moveTo(10,5);path.lineTo(11,7);path.moveTo(13,17);path.lineTo(14,19); }
        break;
    }
    case ButtonGlyph::SignOut:
        path.moveTo(10,4);path.lineTo(4,4);path.lineTo(4,20);path.lineTo(10,20);
        path.moveTo(10,12);path.lineTo(21,12);path.moveTo(16,7);path.lineTo(21,12);path.lineTo(16,17);
        break;
    case ButtonGlyph::Refresh:
        path.moveTo(20,10);path.cubicTo(19,6,16,3.5,12,4);
        path.cubicTo(7.6,4,4,7.6,4,12);path.cubicTo(4,16.4,7.6,20,12,20);
        path.cubicTo(15,20,17.4,19,19,17);
        path.moveTo(20,4);path.lineTo(20,10);path.lineTo(14,10);
        break;
    case ButtonGlyph::Close:
        path.moveTo(6,6);path.lineTo(18,18);path.moveTo(18,6);path.lineTo(6,18);
        break;
    case ButtonGlyph::Preparing:
        path.moveTo(6,3);path.lineTo(18,3);path.moveTo(6,21);path.lineTo(18,21);
        path.moveTo(7,3);path.lineTo(7,7);path.cubicTo(7,9,9,10,12,12);
        path.cubicTo(9,14,7,15,7,17);path.lineTo(7,21);
        path.moveTo(17,3);path.lineTo(17,7);path.cubicTo(17,9,15,10,12,12);
        path.cubicTo(15,14,17,15,17,17);path.lineTo(17,21);
        path.moveTo(9,18);path.lineTo(15,18);
        break;
    case ButtonGlyph::Back:
        path.moveTo(20,12);path.lineTo(4,12);path.moveTo(11,5);path.lineTo(4,12);path.lineTo(11,19);
        break;
    case ButtonGlyph::Up:
        path.moveTo(12,20);path.lineTo(12,4);path.moveTo(5,11);path.lineTo(12,4);path.lineTo(19,11);
        break;
    case ButtonGlyph::Down:
        path.moveTo(12,4);path.lineTo(12,20);path.moveTo(5,13);path.lineTo(12,20);path.lineTo(19,13);
        break;
    }
    p.drawPath(path);
    return pixmap;
}
}

QSize buttonIconSize(int edge) {
    // Qt adds a fixed four-pixel label gap; extend it proportionally for e-ink.
    return QSize(edge+qMax(6,edge*7/16)-4,edge);
}

QIcon buttonIcon(ButtonGlyph glyph, int size, bool pressed) {
    QIcon icon(drawGlyph(glyph,size,pressed?QColor(Qt::white):QColor(Qt::black)));
    icon.addPixmap(drawGlyph(glyph,size,QColor("#777777")),QIcon::Disabled);
    return icon;
}
} // namespace tailui
