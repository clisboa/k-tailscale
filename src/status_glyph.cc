#include "status_glyph.h"
#include <QPainter>
namespace tailui {
void paintStatusGlyph(QPainter &painter,bool connected,bool attention) {
    painter.save();painter.setRenderHint(QPainter::Antialiasing);
    painter.setPen(QPen(Qt::black,2.0));
    for(int row=0;row<3;++row)for(int col=0;col<3;++col){
        painter.setBrush(connected&&(row==0||col==1)?Qt::black:Qt::white);
        painter.drawEllipse(QPointF(7+col*14,7+row*14),3.8,3.8);
    }
    if(attention){
        painter.setBrush(Qt::white);painter.drawEllipse(QPointF(35,35),7,7);
        painter.drawLine(QPointF(35,30),QPointF(35,35));painter.drawPoint(QPointF(35,39));
    }
    painter.restore();
}
}
