#include "qr_code.h"
#include "qr_license.h"
#include "vendor/qrcodegen.hpp"
#include <QPainter>
#include <QUrl>
#include <exception>

namespace tailui {
QImage loginQr(const QString &url,int maxPixels) {
    const QByteArray bytes=url.toUtf8();
    const QUrl parsed(url);
    if(bytes.isEmpty()||bytes.size()>2331||bytes.contains('\0')||!parsed.isValid()||parsed.host().isEmpty()||
        (parsed.scheme()!="https"&&parsed.scheme()!="http"))return {};
    const int pixels=qMin(maxPixels,420),quiet=4,minScale=4;
    const int maxVersion=qMin(20,(pixels/minScale-quiet*2-17)/4);
    if(maxVersion<1)return {};
    try {
        const auto segments=qrcodegen::QrSegment::makeSegments(bytes.constData());
        const auto code=qrcodegen::QrCode::encodeSegments(segments,qrcodegen::QrCode::Ecc::MEDIUM,1,maxVersion);
        const int modules=code.getSize()+quiet*2,scale=pixels/modules;
        if(scale<minScale)return {};
        QImage image(modules*scale,modules*scale,QImage::Format_RGB32);
        if(image.isNull())return {};
        image.fill(Qt::white);QPainter painter(&image);
        for(int y=0;y<code.getSize();++y)for(int x=0;x<code.getSize();++x)
            if(code.getModule(x,y))painter.fillRect((x+quiet)*scale,(y+quiet)*scale,scale,scale,Qt::black);
        return image;
    } catch(const std::exception &) {
        return {};
    }
}
}
