// Qt plugin-loading pattern adapted from Relio, Apache-2.0. See NOTICE.
#pragma once
#include <QImageIOPlugin>
class TailscaleUiImagePlugin : public QImageIOPlugin {
    Q_OBJECT
    Q_PLUGIN_METADATA(IID "org.qt-project.Qt.QImageIOHandlerFactoryInterface" FILE "image_plugin.json")
public:
    Capabilities capabilities(QIODevice *,const QByteArray &) const override {return 0;}
    QImageIOHandler *create(QIODevice *,const QByteArray & = QByteArray()) const override {return nullptr;}
};
