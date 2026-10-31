#include "qr_widget.h"
#include "qr_code.h"
#include <QPainter>
namespace tailui {
QrWidget::QrWidget(QWidget *parent):QWidget(parent){
    setSizePolicy(QSizePolicy::Fixed,QSizePolicy::Fixed);
    setAttribute(Qt::WA_OpaquePaintEvent);
}
bool QrWidget::setUrl(const QString &url,int maxPixels){
    const int pixels=qMin(maxPixels,420);
    if(url_==url&&pixels_==pixels)return !image_.isNull();
    url_=url;pixels_=pixels;image_=loginQr(url,pixels);
    setFixedSize(image_.isNull()?QSize(0,0):image_.size());update();
    return !image_.isNull();
}
void QrWidget::paintEvent(QPaintEvent *){
    QPainter painter(this);painter.fillRect(rect(),Qt::white);
    painter.drawImage(QPoint(0,0),image_);
}
}
